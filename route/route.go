package route

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	reSplitOR   = regexp.MustCompile(`\s*\|\s*`)
	reMatchVars = regexp.MustCompile(`\{(?P<key>\w+)(?:\:\s*(?P<value>[^\}]*)|)\s*\}`)
)

const defaultVarPattern = `[^/]+` // implicit pattern in "{name}" (no ":pattern")

type Route struct {
	Conditions  CondFlags
	Pattern     *regexp.Regexp
	Vars        map[string]int // name => match position
	EntryPrefix string
	IsPrefix    bool
	Handler     interface{}
}

func (r *Route) String() string {
	pattern := r.EntryPrefix
	if r.Pattern != nil {
		pattern = r.Pattern.String()
	}
	return fmt.Sprintf("{%s %s}", r.Conditions, pattern)
}

func (r *Route) Parse(pathPattern string) error {
	// parse: "COND|COND /path/pattern" -> {{"COND", "COND"}, "path/pattern"}
	pathPattern = strings.TrimSpace(pathPattern)
	i := strings.IndexByte(pathPattern, '/')
	if i == -1 {
		return fmt.Errorf("invalid route pattern %q; missing leading \"/\" in path", pathPattern)
	}
	var conditions []string
	condstr := strings.Trim(pathPattern[:i], "| \t\r\n")
	if len(condstr) > 0 {
		conditions = reSplitOR.Split(condstr, -1)
		pathPattern = pathPattern[i:]
	}
	if len(pathPattern) == 0 {
		return fmt.Errorf("empty route pattern")
	}

	// parse conditions
	conds, err := ParseCondFlags(conditions)
	if err != nil {
		return err
	}
	r.Conditions = conds

	// prefix? i.e. "/foo/" is a prefix while "/foo" and "/foo/!" are not.
	c := pathPattern[len(pathPattern)-1]
	if c == '/' {
		r.IsPrefix = true
	} else if c == '!' {
		// "/foo/!" => "/foo/"
		// "/foo/!!" => "/foo/!"
		pathPattern = pathPattern[:len(pathPattern)-1]
	}

	// find vars
	pathPatternBytes := []byte(pathPattern)
	locations := reMatchVars.FindAllSubmatchIndex(pathPatternBytes, -1)
	if len(locations) == 0 {
		// no vars
		r.EntryPrefix = pathPattern
		r.Vars = nil
		r.Pattern = nil
		return nil
	}

	// has vars; will build r.Pattern
	r.EntryPrefix = ""
	r.Vars = make(map[string]int, len(locations))
	resultPattern := make([]byte, 1, len(pathPatternBytes)*2)
	resultPattern[0] = '^'
	plainStart := 0

	for varIndex, loc := range locations {
		varStart, varEnd := loc[0], loc[1] // range of whole "{...}" chunk

		// add plain chunk to resultPattern (whatever comes before the var)
		if plainStart < varStart {
			chunk := pathPattern[plainStart:varStart]
			if plainStart == 0 {
				r.EntryPrefix = chunk
			}
			resultPattern = append(resultPattern, regexp.QuoteMeta(chunk)...)
		}
		plainStart = varEnd

		// extract var name and pattern
		varName := pathPattern[loc[2]:loc[3]]
		pat := defaultVarPattern
		if loc[4] > -1 {
			// trim away leading "^" and trailing "$" in pattern. E.g. "^\w+$" -> "\w+"
			pat = pathPattern[loc[4]:loc[5]]
			if len(pat) == 0 {
				pat = defaultVarPattern
			}
		}

		// memorize var
		if varName != "_" {
			if _, ok := r.Vars[varName]; ok {
				return fmt.Errorf("duplicate var %q in route pattern %q", varName, pathPattern)
			}
			r.Vars[varName] = varIndex
		}

		// add var capture pattern
		resultPattern = append(resultPattern, '(')
		resultPattern = append(resultPattern, pat...)
		resultPattern = append(resultPattern, ')')
	}

	// add any trailing plain chunk
	if plainStart < len(pathPattern) {
		resultPattern = append(resultPattern, regexp.QuoteMeta(pathPattern[plainStart:])...)
	}

	// terminating "$", unless r.IsPrefix
	if !r.IsPrefix {
		resultPattern = append(resultPattern, '$')
	}

	// compile regexp pattern
	re, err := regexp.Compile(string(resultPattern))
	if err != nil {
		return err
	}
	r.Pattern = re
	return nil
}
