package strategy

import (
	"fmt"
	"strconv"
	"strings"
)

type Fault struct {
	Pointer string
	Message string
}

func (f *Fault) Error() string {
	if f.Pointer == "" {
		return f.Message
	}
	return f.Pointer + ": " + f.Message
}

type path []string

func (p path) child(token string) path {
	next := make(path, len(p), len(p)+1)
	copy(next, p)
	return append(next, token)
}

func (p path) index(at int) path {
	return p.child(strconv.Itoa(at))
}

func (p path) String() string {
	if len(p) == 0 {
		return ""
	}

	var out strings.Builder
	for _, token := range p {
		out.WriteByte('/')
		out.WriteString(escapeToken(token))
	}
	return out.String()
}

func (p path) faultf(format string, args ...any) *Fault {
	return &Fault{Pointer: p.String(), Message: fmt.Sprintf(format, args...)}
}

func escapeToken(token string) string {
	token = strings.ReplaceAll(token, "~", "~0")
	return strings.ReplaceAll(token, "/", "~1")
}
