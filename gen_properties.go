//go:build generate

// This program generates a property file in Go file from Unicode Character
// Database auxiliary data files. The command line arguments are as follows:
//
//   1. The name of the Unicode data file (just the filename, without extension).
//   2. The name of the locally generated Go file.
//   3. The name of the slice mapping code points to properties.
//   4. The name of the generator, for logging purposes.
//
//go:generate go run gen_properties.go GraphemeBreakProperty graphemeproperties.go graphemeCodePoints graphemes
//go:generate go run gen_properties.go WordBreakProperty wordproperties.go workBreakCodePoints words
package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"go/format"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// We want to test against a specific version rather than the latest. When the
// package is upgraded to a new version, change these to generate new tests.
const (
	gbpURL   = `https://www.unicode.org/Public/14.0.0/ucd/auxiliary/%s.txt`
	emojiURL = `https://unicode.org/Public/14.0.0/ucd/emoji/emoji-data.txt`
)

// The regular expression for a line containing a code point range property.
var propertyPattern = regexp.MustCompile(`^([0-9A-F]{4,6})(\.\.([0-9A-F]{4,6}))?\s+;\s+([A-Za-z0-9_]+)\s*#\s(.+)$`)

func main() {
	if len(os.Args) < 5 {
		fmt.Println("Not enough arguments, see code for details")
		os.Exit(1)
	}

	log.SetPrefix("gen_properties (" + os.Args[4] + "): ")
	log.SetFlags(0)

	// Parse the text file and generate Go source code from it.
	src, err := parse(fmt.Sprintf(gbpURL, os.Args[1]), emojiURL)
	if err != nil {
		log.Fatal(err)
	}

	// Format the Go code.
	formatted, err := format.Source([]byte(src))
	if err != nil {
		log.Fatal("gofmt:", err)
	}

	// Save it to the (local) target file.
	log.Print("Writing to ", os.Args[2])
	if err := ioutil.WriteFile(os.Args[2], formatted, 0644); err != nil {
		log.Fatal(err)
	}
}

// parse parses the Unicode Properties text files located at the given URLs and
// returns their equivalent Go source code to be used in the uniseg package.
func parse(gbpURL, emojiURL string) (string, error) {
	// Temporary buffer to hold properties.
	var properties [][4]string

	// Open the first URL.
	log.Printf("Parsing %s", gbpURL)
	res, err := http.Get(gbpURL)
	if err != nil {
		return "", err
	}
	in1 := res.Body
	defer in1.Close()

	// Parse it.
	scanner := bufio.NewScanner(in1)
	num := 0
	for scanner.Scan() {
		num++
		line := strings.TrimSpace(scanner.Text())

		// Skip comments and empty lines.
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		// Everything else must be a code point range, a property and a comment.
		from, to, property, comment, err := parseProperty(line)
		if err != nil {
			return "", fmt.Errorf("%s line %d: %v", os.Args[4], num, err)
		}
		properties = append(properties, [4]string{from, to, property, comment})
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	// Open the second URL.
	log.Printf("Parsing %s", emojiURL)
	res, err = http.Get(emojiURL)
	if err != nil {
		return "", err
	}
	in2 := res.Body
	defer in2.Close()

	// Parse it.
	scanner = bufio.NewScanner(in2)
	num = 0
	for scanner.Scan() {
		num++
		line := scanner.Text()

		// Skip comments, empty lines, and everything not containing
		// "Extended_Pictographic".
		if strings.HasPrefix(line, "#") || line == "" || !strings.Contains(line, "Extended_Pictographic") {
			continue
		}

		// Everything else must be a code point range, a property and a comment.
		from, to, property, comment, err := parseProperty(line)
		if err != nil {
			return "", fmt.Errorf("emojis line %d: %v", num, err)
		}
		properties = append(properties, [4]string{from, to, property, comment})
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}

	// Sort properties.
	sort.Slice(properties, func(i, j int) bool {
		left, _ := strconv.ParseUint(properties[i][0], 16, 64)
		right, _ := strconv.ParseUint(properties[j][0], 16, 64)
		return left < right
	})

	// Header.
	var buf bytes.Buffer
	buf.WriteString(`// Code generated via go generate from gen_properties.go. DO NOT EDIT.
package uniseg

// ` + os.Args[3] + ` are taken from
// ` + gbpURL + `,
// and
// ` + emojiURL + `,
// ("Extended_Pictographic" only) on ` + time.Now().Format("January 2, 2006") + `. See
// https://www.unicode.org/license.html for the Unicode license agreement.
var ` + os.Args[3] + ` = [][3]int{
	`)

	// Properties.
	for _, prop := range properties {
		fmt.Fprintf(&buf, "{0x%s,0x%s,%s}, // %s\n", prop[0], prop[1], translateProperty("pr", prop[2]), prop[3])
	}

	// Tail.
	buf.WriteString("}")

	return buf.String(), nil
}

// parseProperty parses a line of the Unicode properties text file containing a
// property for a code point range and returns it along with its comment.
func parseProperty(line string) (from, to, property, comment string, err error) {
	fields := propertyPattern.FindStringSubmatch(line)
	if fields == nil {
		err = errors.New("no property found")
		return
	}
	from = fields[1]
	to = fields[3]
	if to == "" {
		to = from
	}
	property = fields[4]
	comment = fields[5]
	return
}

// translateProperty translates a property name as used in the Unicode data file
// to a variable used in the Go code.
func translateProperty(prefix, property string) string {
	return prefix + strings.ReplaceAll(property, "_", "")
}