package ws

type AuthError struct {
	Message string
}

type RateLimitError struct {
	Message string
}

type ConnectionError struct {
	Message string
}

func (e *AuthError) Error() string       { return e.Message }
func (e *RateLimitError) Error() string  { return e.Message }
func (e *ConnectionError) Error() string { return e.Message }
