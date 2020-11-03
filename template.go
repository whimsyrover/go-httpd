package httpd

import (
	"bytes"
	html_template "html/template"
	"io"
	"io/ioutil"
	"path/filepath"
	text_template "text/template"
	tparse "text/template/parse"
)

type Template interface {
	Name() string
	AddParseTree(name string, tree *tparse.Tree) (Template, error) // returns possibly new template
	Option(option string)
	Funcs(funcs map[string]interface{})
	Exec(w io.Writer, data interface{}) error
	ExecNamed(w io.Writer, name string, data interface{}) error
	ExecBuf(data interface{}) ([]byte, error)
	Templates() []Template
	Tree() *tparse.Tree
}

func NewHtmlTemplate(name string) Template {
	return &htmlTemplate{html_template.New(name)}
}

func NewTextTemplate(name string) Template {
	return &textTemplate{text_template.New(name)}
}

func ParseHtmlTemplate(name, text string) (t Template, err error) {
	tpl := html_template.New(name)
	tpl.Funcs(standardTemplateHelpers())
	if _, err := tpl.Parse(text); err != nil {
		return nil, err
	}
	return &htmlTemplate{tpl}, nil
}

func ParseTextTemplate(name, text string) (Template, error) {
	tpl := text_template.New(name)
	tpl.Funcs(standardTemplateHelpers())
	if _, err := tpl.Parse(text); err != nil {
		return nil, err
	}
	return &textTemplate{tpl}, nil
}

func ParseHtmlTemplateFile(filename string) (t Template, err error) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return ParseHtmlTemplate(filepath.Base(filename), string(b))
}

func ParseTextTemplateFile(filename string) (t Template, err error) {
	b, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return ParseTextTemplate(filepath.Base(filename), string(b))
}

// func parseTemplate(name string, text string) (*tparse.Tree, error) {
//   helpers := standardTemplateHelpers()
//   asts, err := tparse.Parse(name, text, "{{", "}}", helpers)
//   if err != nil {
//     return nil, err
//   }
//   return asts[name], nil
// }

// ------------------------------------------------------------------------

type htmlTemplate struct {
	t *html_template.Template
}

func (t *htmlTemplate) AddParseTree(name string, tree *tparse.Tree) (Template, error) {
	t2, err := t.t.AddParseTree(name, tree)
	if err != nil {
		return nil, err
	}
	return &htmlTemplate{t2}, err
}

func (t *htmlTemplate) Name() string                             { return t.t.Name() }
func (t *htmlTemplate) Option(option string)                     { t.t.Option(option) }
func (t *htmlTemplate) Funcs(funcs map[string]interface{})       { t.t.Funcs(funcs) }
func (t *htmlTemplate) Exec(w io.Writer, data interface{}) error { return t.t.Execute(w, data) }
func (t *htmlTemplate) ExecNamed(w io.Writer, name string, data interface{}) error {
	return t.t.ExecuteTemplate(w, name, data)
}
func (t *htmlTemplate) Tree() *tparse.Tree { return t.t.Tree }

func (t *htmlTemplate) ExecBuf(data interface{}) ([]byte, error) {
	var buf bytes.Buffer
	err := t.Exec(&buf, data)
	return buf.Bytes(), err
}

func (t *htmlTemplate) Templates() []Template {
	src := t.t.Templates()
	tv := make([]Template, len(src))
	for i, t2 := range src {
		tv[i] = &htmlTemplate{t2}
	}
	return tv
}

// ------------------------------------------------------------------------

type textTemplate struct {
	t *text_template.Template
}

func (t *textTemplate) AddParseTree(name string, tree *tparse.Tree) (Template, error) {
	t2, err := t.t.AddParseTree(name, tree)
	if err != nil {
		return nil, err
	}
	return &textTemplate{t2}, err
}

func (t *textTemplate) Name() string                             { return t.t.Name() }
func (t *textTemplate) Option(option string)                     { t.t.Option(option) }
func (t *textTemplate) Funcs(funcs map[string]interface{})       { t.t.Funcs(funcs) }
func (t *textTemplate) Exec(w io.Writer, data interface{}) error { return t.t.Execute(w, data) }
func (t *textTemplate) ExecNamed(w io.Writer, name string, data interface{}) error {
	return t.t.ExecuteTemplate(w, name, data)
}
func (t *textTemplate) Tree() *tparse.Tree { return t.t.Tree }

func (t *textTemplate) ExecBuf(data interface{}) ([]byte, error) {
	var buf bytes.Buffer
	err := t.Exec(&buf, data)
	return buf.Bytes(), err
}

func (t *textTemplate) Templates() []Template {
	src := t.t.Templates()
	tv := make([]Template, len(src))
	for i, t2 := range src {
		tv[i] = &textTemplate{t2}
	}
	return tv
}
