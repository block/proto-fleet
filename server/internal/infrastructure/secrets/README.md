# Secrets
This is a package meant to protect the accidental logging of secret values within a golang application.

## Example Usage
```go
type UserCreds {
    Username string
    Name string
    Password *secrets.Text
    Secret secrets.Text
}

func DoSomethingForUser(userCreds UserCreds) bool {
    ...
    if err := nil {
        slog.Error("something broke", "error", err, "user", userCreds)
        return false
    }
    ...
}
```

```shell
ERROR something broke error="failed to do something with" user="&{UserName:johndoe Name:John Doe Password:*********** Secret:***********}"
```
