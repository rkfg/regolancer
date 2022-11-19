package helpmessage

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/jessevdk/go-flags"
)

const (
	paddingBeforeOption                 = 2
	distanceBetweenOptionAndDescription = 2
	defaultShortOptDelimiter            = '-'
	defaultLongOptDelimiter             = "--"
	defaultNameArgDelimiter             = '='
)

type Option struct {
	Description string

	ShortName rune

	LongName string

	Grouping string

	field reflect.StructField

	value reflect.Value
}

type Options struct {
	options []*Option
}

func ScanStruct(realval reflect.Value, opt *Options) error {

	stype := realval.Type()

	for i := 0; i < stype.NumField(); i++ {
		field := stype.Field(i)

		longname := field.Tag.Get("long")
		shortname := field.Tag.Get("short")
		grouping := field.Tag.Get("rego-grouping")
		description := field.Tag.Get("description")

		// Need at least either a short or long name
		if longname == "" && shortname == "" {
			continue
		}

		short := rune(0)
		rc := utf8.RuneCountInString(shortname)

		if rc > 1 {
			return fmt.Errorf("short names can only be 1 character long, not `%s'",
				shortname)

		} else if rc == 1 {
			short, _ = utf8.DecodeRuneInString(shortname)
		}

		option := &Option{
			Description: description,
			ShortName:   short,
			LongName:    longname,
			Grouping:    grouping,

			field: field,
			value: realval.Field(i),
		}

		opt.options = append(opt.options, option)
	}

	return nil
}

func WriteHelp(options *Options, p *flags.Parser, writer io.Writer) (err error) {
	if writer == nil {
		return
	}

	wr := bufio.NewWriter(os.Stdout)
	aligninfo := getAlignmentInfo(options, p)

	if p.Name != "" {
		wr.WriteString("Usage:\n")
		wr.WriteString(" ")
		fmt.Fprintf(wr, " %s [OPTIONS]\n", p.Name)
	}

	fmt.Fprintf(wr, "\nApplication Options:\n")
	fmt.Fprintf(wr, "--------------------")
	aligninfo.indent = false
	for _, info := range options.options {
		if !info.showInHelp() {
			continue
		}

		writeHelpOption(wr, info, aligninfo)
	}

	wr.Flush()
	return nil
}

// }

func getAlignmentInfo(options *Options, p *flags.Parser) alignmentInfo {
	ret := alignmentInfo{
		maxLongLen:      0,
		hasShort:        false,
		hasValueName:    false,
		terminalColumns: getTerminalColumns(),
	}

	if ret.terminalColumns <= 0 {
		ret.terminalColumns = 80
	}

	for _, info := range options.options {
		if !info.showInHelp() {
			continue
		}

		if info.ShortName != 0 {
			ret.hasShort = true
		}

		l := info.LongName

		ret.updateLen(l, false)
	}

	return ret
}

type alignmentInfo struct {
	maxLongLen      int
	hasShort        bool
	hasValueName    bool
	terminalColumns int
	indent          bool
}

const (
	HelpFlag = 1
)

func wrapText(s string, l int, prefix string) string {
	var ret string

	if l < 10 {
		l = 10
	}

	// Basic text wrapping of s at spaces to fit in l
	lines := strings.Split(s, "\n")

	for _, line := range lines {
		var retline string

		line = strings.TrimSpace(line)

		for len(line) > l {
			// Try to split on space
			suffix := ""

			pos := strings.LastIndex(line[:l], " ")

			if pos < 0 {
				pos = l - 1
				suffix = "-\n"
			}

			if len(retline) != 0 {
				retline += "\n" + prefix

			}

			retline += strings.TrimSpace(line[:pos]) + suffix
			line = strings.TrimSpace(line[pos:])
		}

		if len(line) > 0 {
			if len(retline) != 0 {
				retline += "\n" + prefix
			}

			retline += line
		}

		if len(ret) > 0 {
			ret += "\n"

			if len(retline) > 0 {
				ret += prefix
			}
		}

		ret += retline
	}

	return ret
}

func (option *Option) showInHelp() bool {
	return (option.ShortName != 0 || len(option.LongName) != 0)
}

func (a *alignmentInfo) descriptionStart() int {
	ret := a.maxLongLen + distanceBetweenOptionAndDescription

	if a.hasShort {
		ret += 2
	}

	if a.maxLongLen > 0 {
		ret += 4
	}

	if a.hasValueName {
		ret += 3
	}

	return ret
}

func (a *alignmentInfo) updateLen(name string, indent bool) {
	l := utf8.RuneCountInString(name)

	if indent {
		l = l + 4
	}

	if l > a.maxLongLen {
		a.maxLongLen = l
	}
}

var group = make(map[string]bool)

func writeHelpOption(writer *bufio.Writer, option *Option, info alignmentInfo) {
	line := &bytes.Buffer{}

	if _, ok := group[option.Grouping]; !ok && option.Grouping != "" {
		writer.WriteString(fmt.Sprintf("\n%s:\n", option.Grouping))
		group[option.Grouping] = true
	}

	prefix := paddingBeforeOption

	if info.indent {
		prefix += 4
	}

	line.WriteString(strings.Repeat(" ", prefix))

	if option.ShortName != 0 {
		line.WriteRune(defaultShortOptDelimiter)
		line.WriteRune(option.ShortName)
	} else if info.hasShort {
		line.WriteString("  ")
	}

	descstart := info.descriptionStart() + paddingBeforeOption

	if len(option.LongName) > 0 {
		if option.ShortName != 0 {
			line.WriteString(", ")
		} else if info.hasShort {
			line.WriteString("  ")
		}

		line.WriteString(defaultLongOptDelimiter)
		line.WriteString(option.LongName)
	}

	written := line.Len()
	line.WriteTo(writer)

	if option.Description != "" {
		dw := descstart - written

		writer.WriteString(strings.Repeat(" ", dw))

		desc := option.Description

		writer.WriteString(wrapText(desc,
			info.terminalColumns-descstart,
			strings.Repeat(" ", descstart)))
	}

	writer.WriteString("\n")
}
