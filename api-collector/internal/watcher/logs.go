package watcher

type LogLine []byte

type Stream struct {
	Lines chan LogLine
}

func NewStream(bufferSize int) *Stream {
	return &Stream{
		Lines: make(chan LogLine, bufferSize),
	}
}
