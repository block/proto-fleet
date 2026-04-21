using System.Net;
using System.Security.Cryptography;
using System.Security.Cryptography.X509Certificates;
using System.Text;

namespace ProtoFleet.Installer.Core.Services;

public sealed class NginxConfigurator : INginxConfigurator
{
    private readonly ILogSink _logSink;

    public NginxConfigurator(ILogSink logSink)
    {
        _logSink = logSink;
    }

    public Task<InstallerStepResult> ConfigureAsync(InstallerContext context, CancellationToken cancellationToken)
    {
        cancellationToken.ThrowIfCancellationRequested();

        if (string.IsNullOrWhiteSpace(context.DeploymentRootWindowsPath))
        {
            return Task.FromResult(InstallerStepResult.Failed("Deployment root path is missing."));
        }

        var root = context.DeploymentRootWindowsPath;
        var envPath = Path.Combine(root, ".env");
        if (!File.Exists(envPath))
        {
            return Task.FromResult(InstallerStepResult.Failed("Expected .env to exist before nginx configuration."));
        }

        var sslDir = Path.Combine(root, "ssl");
        Directory.CreateDirectory(sslDir);

        var certPath = Path.Combine(sslDir, "cert.pem");
        var keyPath = Path.Combine(sslDir, "key.pem");

        context.EffectiveProtocolMode = ResolveProtocolMode(context, certPath, keyPath);

        if (context.EffectiveProtocolMode == ProtocolMode.HttpsSelfSigned)
        {
            CreateSelfSignedCertificate(certPath, keyPath);
        }

        if (context.EffectiveProtocolMode == ProtocolMode.HttpsExisting)
        {
            if (string.IsNullOrWhiteSpace(context.Options.ExistingCertPath) || string.IsNullOrWhiteSpace(context.Options.ExistingKeyPath))
            {
                return Task.FromResult(InstallerStepResult.Failed("HTTPS existing cert mode requires cert and key path."));
            }

            var sourceCertPath = Path.GetFullPath(context.Options.ExistingCertPath);
            var sourceKeyPath = Path.GetFullPath(context.Options.ExistingKeyPath);
            if (!File.Exists(sourceCertPath))
            {
                return Task.FromResult(InstallerStepResult.Failed(
                    $"HTTPS existing cert mode requires a readable cert file. Missing: {sourceCertPath}"));
            }

            if (!File.Exists(sourceKeyPath))
            {
                return Task.FromResult(InstallerStepResult.Failed(
                    $"HTTPS existing cert mode requires a readable key file. Missing: {sourceKeyPath}"));
            }

            File.Copy(sourceCertPath, certPath, overwrite: true);
            File.Copy(sourceKeyPath, keyPath, overwrite: true);
        }

        var sourceName = context.EffectiveProtocolMode == ProtocolMode.Http ? "nginx.http.conf" : "nginx.https.conf";
        var source = Path.Combine(root, "client", sourceName);
        var target = Path.Combine(root, "client", "nginx.conf");
        if (!File.Exists(source))
        {
            return Task.FromResult(InstallerStepResult.Failed($"Expected nginx profile '{sourceName}' was not found."));
        }

        File.Copy(source, target, overwrite: true);

        var values = EnvFile.Parse(envPath);
        values["SESSION_COOKIE_SECURE"] = context.EffectiveProtocolMode == ProtocolMode.Http ? "false" : "true";
        EnvFile.Write(envPath, values);

        _logSink.Info($"Configured protocol mode: {context.EffectiveProtocolMode}");
        return Task.FromResult(InstallerStepResult.Succeeded());
    }

    private static void CreateSelfSignedCertificate(string certPath, string keyPath)
    {
        using var rsa = RSA.Create(2048);
        var request = new CertificateRequest("CN=localhost", rsa, HashAlgorithmName.SHA256, RSASignaturePadding.Pkcs1);
        var sanBuilder = new SubjectAlternativeNameBuilder();
        sanBuilder.AddDnsName("localhost");
        sanBuilder.AddDnsName(Environment.MachineName);
        foreach (var ip in Dns.GetHostEntry(Dns.GetHostName()).AddressList.Where(ip => !IPAddress.IsLoopback(ip)))
        {
            sanBuilder.AddIpAddress(ip);
        }

        request.CertificateExtensions.Add(sanBuilder.Build());
        request.CertificateExtensions.Add(new X509BasicConstraintsExtension(false, false, 0, false));
        request.CertificateExtensions.Add(new X509KeyUsageExtension(X509KeyUsageFlags.DigitalSignature | X509KeyUsageFlags.KeyEncipherment, false));
        request.CertificateExtensions.Add(new X509SubjectKeyIdentifierExtension(request.PublicKey, false));

        using var certificate = request.CreateSelfSigned(DateTimeOffset.UtcNow.AddDays(-1), DateTimeOffset.UtcNow.AddDays(365));
        var certPem = PemEncoding.WriteString("CERTIFICATE", certificate.Export(X509ContentType.Cert));
        var keyPem = PemEncoding.WriteString("PRIVATE KEY", rsa.ExportPkcs8PrivateKey());
        File.WriteAllText(certPath, certPem, Encoding.ASCII);
        File.WriteAllText(keyPath, keyPem, Encoding.ASCII);
    }

    private static ProtocolMode ResolveProtocolMode(InstallerContext context, string certPath, string keyPath)
    {
        if (context.Options.SetupMode == SetupMode.Simple)
        {
            return ProtocolMode.Http;
        }

        return context.Options.ProtocolMode switch
        {
            ProtocolMode.Http => ProtocolMode.Http,
            ProtocolMode.HttpsSelfSigned => ProtocolMode.HttpsSelfSigned,
            ProtocolMode.HttpsExisting => ProtocolMode.HttpsExisting,
            _ => File.Exists(certPath) && File.Exists(keyPath) ? ProtocolMode.HttpsExisting : ProtocolMode.Http,
        };
    }
}
