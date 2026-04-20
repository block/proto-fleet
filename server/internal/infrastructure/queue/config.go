package queue

type Config struct {
	DequeLimit        int32 `help:"How many messages will maximally one deque call return." default:"500" env:"DEQUE_LIMIT"`
	MaxFailureRetries int32 `help:"How many times can a message fail before it is thrown away and marked FAILED." default:"5" env:"MAX_FAILURE_RETRIES"`
}
