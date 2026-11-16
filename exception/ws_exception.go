package exception

const (
	codeWSGoingAway       = 1001
	codeWSProtocolError   = 1002
	codeWSUnsupportedData = 1003
	codeWSInvalidPayload  = 1007
	codeWSPolicyViolation = 1008
	codeWSMessageTooBig   = 1009
	codeWSInternalError   = 1011

	codeWSNotSubscribed = 4001
	codeWSTopicNotFound = 4004
)

func GoingAwayException(message string, opts ...any) Exception {
	return NewException(message, codeWSGoingAway, opts...)
}

func ProtocolErrorException(message string, opts ...any) Exception {
	return NewException(message, codeWSProtocolError, opts...)
}

func UnsupportedDataException(message string, opts ...any) Exception {
	return NewException(message, codeWSUnsupportedData, opts...)
}

func InvalidPayloadException(message string, opts ...any) Exception {
	return NewException(message, codeWSInvalidPayload, opts...)
}

func PolicyViolationException(message string, opts ...any) Exception {
	return NewException(message, codeWSPolicyViolation, opts...)
}

func MessageTooBigException(message string, opts ...any) Exception {
	return NewException(message, codeWSMessageTooBig, opts...)
}

func WSInternalErrorException(message string, opts ...any) Exception {
	return NewException(message, codeWSInternalError, opts...)
}

func NotSubscribedException(message string, opts ...any) Exception {
	return NewException(message, codeWSNotSubscribed, opts...)
}

func TopicNotFoundException(message string, opts ...any) Exception {
	return NewException(message, codeWSTopicNotFound, opts...)
}
