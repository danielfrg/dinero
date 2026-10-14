package ledger

type ErrInvalidInput struct{ Field, Reason string }

func (e *ErrInvalidInput) Error() string { return "invalid " + e.Field + ": " + e.Reason }

type ErrUnknownAccount struct{ Account string }

func (e *ErrUnknownAccount) Error() string { return "unknown account: " + e.Account }

type ErrUnavailable struct{}

func (e *ErrUnavailable) Error() string { return "database unavailable" }
