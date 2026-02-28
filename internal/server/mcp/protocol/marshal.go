package protocol

import "encoding/json"

// OKResponse builds a successful JSON-RPC 2.0 response.
// result is marshaled to JSON and stored in the Result field.
func OKResponse(id any, result any) Response {
	b, _ := json.Marshal(result)
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  json.RawMessage(b),
	}
}

// ErrorResponse builds a JSON-RPC 2.0 error response.
func ErrorResponse(id any, code int, message string, data any) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}
