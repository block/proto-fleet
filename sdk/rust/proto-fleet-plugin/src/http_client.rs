use crate::errors::PluginError;

/// Maps a `reqwest::Error` to the appropriate `PluginError` variant based on the
/// HTTP status code or error kind.
///
/// Error messages are sanitized to avoid leaking internal device URLs/paths across
/// the plugin boundary. Only the device ID and HTTP status code are included.
pub fn map_reqwest_error(err: reqwest::Error, device_id: &str) -> PluginError {
    if let Some(status) = err.status() {
        match status.as_u16() {
            401 | 403 => PluginError::AuthenticationFailed {
                device_id: device_id.to_string(),
            },
            404 => PluginError::DeviceNotFound {
                device_id: device_id.to_string(),
            },
            400 => PluginError::InvalidConfig {
                message: format!("bad request for device {device_id}"),
            },
            code => PluginError::Network {
                message: format!("request failed for device {device_id}: HTTP {code}"),
                source: Some(Box::new(err)),
            },
        }
    } else if err.is_timeout() || err.is_connect() {
        PluginError::DeviceUnavailable {
            device_id: device_id.to_string(),
            source: Some(Box::new(err)),
        }
    } else {
        PluginError::Network {
            message: format!("request failed for device {device_id}"),
            source: Some(Box::new(err)),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const DEVICE_ID: &str = "test-miner-01";

    /// Starts a minimal HTTP server that replies with the given status code and
    /// returns its address. The listener is kept alive via the returned handle.
    async fn status_server(status: u16) -> (String, tokio::task::JoinHandle<()>) {
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let handle = tokio::spawn(async move {
            if let Ok((mut stream, _)) = listener.accept().await {
                // Read the request before responding so hyper doesn't see a
                // premature close. We just need to consume until the blank line.
                let mut buf = vec![0u8; 4096];
                let _ = tokio::io::AsyncReadExt::read(&mut stream, &mut buf).await;

                let response = format!(
                    "HTTP/1.1 {status} X\r\nconnection: close\r\ncontent-length: 0\r\n\r\n"
                );
                let _ = tokio::io::AsyncWriteExt::write_all(&mut stream, response.as_bytes()).await;
                let _ = tokio::io::AsyncWriteExt::shutdown(&mut stream).await;
            }
        });
        (format!("http://{addr}"), handle)
    }

    /// Helper: make a request to the given URL and extract the reqwest::Error
    /// from error_for_status().
    async fn get_status_error(url: &str) -> reqwest::Error {
        reqwest::get(url)
            .await
            .unwrap()
            .error_for_status()
            .unwrap_err()
    }

    #[tokio::test]
    async fn test_400_maps_to_invalid_config() {
        // Arrange
        let (url, _h) = status_server(400).await;

        // Act
        let err = get_status_error(&url).await;
        let plugin_err = map_reqwest_error(err, DEVICE_ID);

        // Assert
        assert!(
            matches!(plugin_err, PluginError::InvalidConfig { ref message } if message.contains(DEVICE_ID)),
            "expected InvalidConfig, got: {plugin_err:?}"
        );
    }

    #[tokio::test]
    async fn test_401_maps_to_auth_failed() {
        // Arrange
        let (url, _h) = status_server(401).await;

        // Act
        let plugin_err = map_reqwest_error(get_status_error(&url).await, DEVICE_ID);

        // Assert
        assert!(matches!(
            plugin_err,
            PluginError::AuthenticationFailed { .. }
        ));
    }

    #[tokio::test]
    async fn test_403_maps_to_auth_failed() {
        // Arrange
        let (url, _h) = status_server(403).await;

        // Act
        let plugin_err = map_reqwest_error(get_status_error(&url).await, DEVICE_ID);

        // Assert
        assert!(matches!(
            plugin_err,
            PluginError::AuthenticationFailed { .. }
        ));
    }

    #[tokio::test]
    async fn test_404_maps_to_device_not_found() {
        // Arrange
        let (url, _h) = status_server(404).await;

        // Act
        let plugin_err = map_reqwest_error(get_status_error(&url).await, DEVICE_ID);

        // Assert
        assert!(matches!(plugin_err, PluginError::DeviceNotFound { .. }));
    }

    #[tokio::test]
    async fn test_500_maps_to_network() {
        // Arrange
        let (url, _h) = status_server(500).await;

        // Act
        let plugin_err = map_reqwest_error(get_status_error(&url).await, DEVICE_ID);

        // Assert
        assert!(
            matches!(plugin_err, PluginError::Network { ref message, .. } if message.contains("500")),
            "expected Network with status 500, got: {plugin_err:?}"
        );
    }

    #[tokio::test]
    async fn test_connect_error_maps_to_device_unavailable() {
        // Arrange — bind a port then drop the listener so nothing is listening
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        drop(listener);

        // Act
        let err = reqwest::get(format!("http://{addr}")).await.unwrap_err();
        let plugin_err = map_reqwest_error(err, DEVICE_ID);

        // Assert
        assert!(
            matches!(plugin_err, PluginError::DeviceUnavailable { .. }),
            "expected DeviceUnavailable, got: {plugin_err:?}"
        );
    }

    #[tokio::test]
    async fn test_timeout_maps_to_device_unavailable() {
        // Arrange — server that accepts but never responds
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let _hold = tokio::spawn(async move {
            let (_stream, _) = listener.accept().await.unwrap();
            tokio::time::sleep(std::time::Duration::from_secs(60)).await;
        });

        let client = reqwest::Client::builder()
            .timeout(std::time::Duration::from_millis(50))
            .build()
            .unwrap();

        // Act
        let err = client
            .get(format!("http://{addr}"))
            .send()
            .await
            .unwrap_err();
        let plugin_err = map_reqwest_error(err, DEVICE_ID);

        // Assert
        assert!(
            matches!(plugin_err, PluginError::DeviceUnavailable { .. }),
            "expected DeviceUnavailable, got: {plugin_err:?}"
        );
    }

    #[tokio::test]
    async fn test_error_messages_do_not_leak_urls() {
        // Arrange
        let (url, _h) = status_server(500).await;

        // Act
        let plugin_err = map_reqwest_error(get_status_error(&url).await, DEVICE_ID);

        // Assert — message should contain device ID and status code, not the URL
        if let PluginError::Network { ref message, .. } = plugin_err {
            assert!(message.contains(DEVICE_ID));
            assert!(message.contains("500"));
            assert!(
                !message.contains("127.0.0.1"),
                "message should not contain URL: {message}"
            );
        } else {
            panic!("expected Network, got: {plugin_err:?}");
        }
    }
}
