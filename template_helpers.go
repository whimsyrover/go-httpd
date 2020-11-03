package httpd

import (
	"fmt"
	"path"
	"strings"
	"sync"
	"time"
)

type TemplateHelpersMap = map[string]interface{}

func NewTemplateHelpersMap(base TemplateHelpersMap) TemplateHelpersMap {
	h := make(TemplateHelpersMap)
	for k, v := range base {
		h[k] = v
	}
	return h
}

var (
	standardTemplateHelpersOnce sync.Once
	standardTemplateHelpersMap  TemplateHelpersMap
)

func standardTemplateHelpers() TemplateHelpersMap {
	standardTemplateHelpersOnce.Do(func() {
		standardTemplateHelpersMap = buildStandardTemplateHelpers()
	})
	return standardTemplateHelpersMap
}

func buildStandardTemplateHelpers() TemplateHelpersMap {
	// helper functions shared by everything
	h := make(TemplateHelpersMap)

	h["ServerDevMode"] = func() bool {
		return DevMode
	}

	h["now"] = func() time.Time {
		return time.Now()
	}

	h["cat"] = func(args ...interface{}) string {
		var b strings.Builder
		fmt.Fprint(&b, args...)
		return b.String()
	}

	h["url"] = func(args ...string) string {
		return path.Join(args...)
	}

	h["timestamp"] = func(v ...interface{}) int64 {
		if len(v) == 0 {
			return time.Now().UTC().Unix()
		} else {
			if t, ok := v[0].(time.Time); ok {
				return t.UTC().Unix()
			}
		}
		return 0
	}

	return h
}

// ----------------

// func cleanFileName(basedir, name string) string {
//   var fn string
//   if runtime.GOOS == "windows" {
//     name = strings.Replace(name, "/", "\\", -1)
//     fn = filepath.Join(basedir, strings.TrimLeft(name, "\\"))
//   } else {
//     fn = filepath.Join(basedir, strings.TrimLeft(name, "/"))
//   }
//   fn = filepath.Clean(fn)
//   if !strings.HasPrefix(fn, basedir) {
//     return ""
//   }
//   return fn
// }

// func (service *Service) buildHelpers(base TemplateHelpersMap) TemplateHelpersMap {
//   // helper functions shared by everything in the same Ghp instance.
//   h := NewTemplateHelpersMap(base)

//   // readfile reads a file relative to PubDir
//   h["readfile"] = func (name string) (string, error) {
//     fn := cleanFileName(g.config.PubDir, name)
//     if fn == "" {
//       return "", errorf("file not found %v", name)
//     }
//     data, err := ioutil.ReadFile(fn)
//     if err != nil {
//       return "", err
//     }
//     return string(data), nil
//   }

//   return h
// }
