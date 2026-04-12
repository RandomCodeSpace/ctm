package output

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
)

const (
	colorRed     = "\033[0;31m"
	colorGreen   = "\033[0;32m"
	colorYellow  = "\033[1;33m"
	colorCyan    = "\033[0;36m"
	colorMagenta = "\033[0;35m"
	colorDim     = "\033[2m"
	colorBold    = "\033[1m"
	colorReset   = "\033[0m"
)

var ansiRegex = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiRegex.ReplaceAllString(s, "")
}

type Printer struct {
	w io.Writer
}

func NewPrinter(w io.Writer) *Printer {
	return &Printer{w: w}
}

func Stdout() *Printer {
	return &Printer{w: os.Stdout}
}

func Stderr() *Printer {
	return &Printer{w: os.Stderr}
}

func (p *Printer) Success(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(p.w, "%s%s%s\n", colorGreen, msg, colorReset)
}

func (p *Printer) Error(what, reason, fix string) {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%sctm: %s%s\n", colorRed, what, colorReset))
	b.WriteString(fmt.Sprintf("  %sreason:%s %s\n", colorDim, colorReset, reason))
	b.WriteString(fmt.Sprintf("  %sfix:%s %s\n", colorDim, colorReset, fix))
	fmt.Fprint(p.w, b.String())
}

func (p *Printer) Warn(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(p.w, "%s%s%s\n", colorYellow, msg, colorReset)
}

func (p *Printer) Info(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(p.w, "%s%s%s\n", colorCyan, msg, colorReset)
}

func (p *Printer) Bold(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(p.w, "%s%s%s\n", colorBold, msg, colorReset)
}

func (p *Printer) Dim(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(p.w, "%s%s%s\n", colorDim, msg, colorReset)
}

func (p *Printer) Magenta(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(p.w, "%s%s%s\n", colorMagenta, msg, colorReset)
}

// Debug prints only when verbose is true. Caller passes the flag.
func (p *Printer) Debug(verbose bool, format string, args ...interface{}) {
	if !verbose {
		return
	}
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(p.w, "%s[debug] %s%s\n", colorDim, msg, colorReset)
}
