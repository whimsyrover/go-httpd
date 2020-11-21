package route

import (
	"fmt"
	"strings"
)

type CondFlags uint64

const (
	CondMethodGET = (1 << iota)
	CondMethodCONNECT
	CondMethodDELETE
	CondMethodHEAD
	CondMethodOPTIONS
	CondMethodPATCH
	CondMethodPOST
	CondMethodPUT
	CondMethodTRACE
)

func (fl CondFlags) String() string {
	if fl == 0 {
		return "*"
	}
	var sb strings.Builder
	if (fl & CondMethodGET) != 0 {
		sb.WriteString("|GET")
	}
	if (fl & CondMethodCONNECT) != 0 {
		sb.WriteString("|CONNECT")
	}
	if (fl & CondMethodDELETE) != 0 {
		sb.WriteString("|DELETE")
	}
	if (fl & CondMethodHEAD) != 0 {
		sb.WriteString("|HEAD")
	}
	if (fl & CondMethodOPTIONS) != 0 {
		sb.WriteString("|OPTIONS")
	}
	if (fl & CondMethodPATCH) != 0 {
		sb.WriteString("|PATCH")
	}
	if (fl & CondMethodPOST) != 0 {
		sb.WriteString("|POST")
	}
	if (fl & CondMethodPUT) != 0 {
		sb.WriteString("|PUT")
	}
	if (fl & CondMethodTRACE) != 0 {
		sb.WriteString("|TRACE")
	}
	b := sb.String()
	if len(b) == 0 {
		return "*"
	}
	return b[1:]
}

func ParseCondFlags(tokens []string) (CondFlags, error) {
	var f CondFlags
	if len(tokens) == 1 && tokens[0] == "*" {
		// special case: "*" for "any" which is the same as no conditions
		return f, nil
	}
	for _, tok := range tokens {
		switch tok {
		case "GET":
			f |= CondMethodGET
		case "CONNECT":
			f |= CondMethodCONNECT
		case "DELETE":
			f |= CondMethodDELETE
		case "HEAD":
			f |= CondMethodHEAD
		case "OPTIONS":
			f |= CondMethodOPTIONS
		case "PATCH":
			f |= CondMethodPATCH
		case "POST":
			f |= CondMethodPOST
		case "PUT":
			f |= CondMethodPUT
		case "TRACE":
			f |= CondMethodTRACE
		default:
			return f, fmt.Errorf("invalid condition %q", tok)
		}
	}
	return f, nil
}
