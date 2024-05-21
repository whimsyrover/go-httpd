package httpd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

func absfile(filename string) string {
	if s, err := filepath.Abs(filename); err == nil {
		return s
	}
	return filename
}

func crashOnErr(err error) {
	if err != nil {
		fatalf(err)
	}
}

func errorf(format string, v ...interface{}) error {
	return fmt.Errorf(format, v...)
}

func fatalf(msg interface{}, arg ...interface{}) {
	var format string
	if s, ok := msg.(string); ok {
		format = s
	} else if s, ok := msg.(fmt.Stringer); ok {
		format = s.String()
	} else {
		format = fmt.Sprintf("%v", msg)
	}
	fmt.Fprintf(os.Stderr, format+"\n", arg...)
	os.Exit(1)
}

var unixEpochTime = time.Unix(0, 0)

// isZeroTime reports whether t is obviously unspecified (either zero or Unix()=0).
func isZeroTime(t time.Time) bool {
	return t.IsZero() || t.Equal(unixEpochTime)
}
