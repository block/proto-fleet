namespace ProtoFleet.Installer.Core;

public static class RetryPolicy
{
    public static async Task<T> ExecuteAsync<T>(
        int attempts,
        Func<int, Task<T>> action,
        Func<T, bool> isSuccess,
        Func<int, TimeSpan>? backoff = null)
    {
        if (attempts <= 0)
        {
            throw new ArgumentOutOfRangeException(nameof(attempts));
        }

        var delayStrategy = backoff ?? (attempt => TimeSpan.FromSeconds(Math.Pow(2, attempt)));
        T? last = default;
        for (var attempt = 1; attempt <= attempts; attempt++)
        {
            last = await action(attempt);
            if (isSuccess(last))
            {
                return last;
            }

            if (attempt < attempts)
            {
                await Task.Delay(delayStrategy(attempt));
            }
        }

        return last!;
    }
}
