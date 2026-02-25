package core

import "errors"

var (
	ErrProjectNotFound   = errors.New("project not found")
	ErrProjectNotIndexed = errors.New("project not indexed")
	ErrPluginNotFound    = errors.New("plugin not found")
	ErrInvalidIR         = errors.New("invalid IR")
	ErrContextWindowFull = errors.New("context window approaching capacity")
	ErrLoopLimitReached  = errors.New("loop limit reached")
	ErrBufferFull        = errors.New("write buffer full")
	ErrTokenRevoked      = errors.New("token revoked")
	ErrTokenExpired      = errors.New("token expired")
	ErrInsufficientScope = errors.New("insufficient token scope")
	ErrReadOnlySession   = errors.New("write attempted in read-only session")
)
