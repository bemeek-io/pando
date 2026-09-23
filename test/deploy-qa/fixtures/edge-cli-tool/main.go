// wordcount prints line, word and byte counts for its input and exits.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

func count(r io.Reader) (lines, words, bytes int, err error) {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		lines++
		words += len(strings.Fields(line))
		bytes += len(line) + 1
	}
	return lines, words, bytes, sc.Err()
}

func main() {
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: wordcount [file ...]")
	}
	flag.Parse()
	if flag.NArg() == 0 {
		l, w, b, err := count(os.Stdin)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%d %d %d\n", l, w, b)
		return
	}
	for _, name := range flag.Args() {
		f, err := os.Open(name)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		l, w, b, err := count(f)
		f.Close()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("%d %d %d %s\n", l, w, b, name)
	}
}
