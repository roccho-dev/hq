package hostopen

type Result struct {
	Executable string
	Args       []string
	PID        int
}

type Opener interface {
	Open(path string) (Result, error)
}

func New() Opener {
	return platformOpener{}
}
