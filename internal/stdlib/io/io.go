package io

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func Module() map[string]any {
	return map[string]any{
		"input":   input,
		"write":   write,
		"writeln": writeln,
	}
}

func readT(s *bufio.Scanner, input any) string {
	fmt.Print(input)
	if s.Scan() {
		input := strings.TrimSpace(s.Text())
		return input
	}
	return ""
}

func appendValue(output *strings.Builder, value any) error {
	if values, ok := value.([]any); ok {
		for _, item := range values {
			if err := appendValue(output, item); err != nil {
				return err
			}
		}
		return nil
	}
	_, err := fmt.Fprintf(output, "%v\t", value)
	return err
}

func input(prompt *string) string {
	p := ""
	if prompt != nil {
		p = *prompt
	}
	s := bufio.NewScanner(os.Stdin)
	return readT(s, p)
}

func write(strs ...any) error {
	var output strings.Builder
	for _, str := range strs {
		if err := appendValue(&output, str); err != nil {
			return fmt.Errorf("io.write: argument '%v': %w", str, err)
		}
	}
	_, err := fmt.Print(output.String())
	return err
}

func writeln(strs ...any) error {
	if err := write(strs); err != nil {
		return err
	}
	fmt.Println()
	return nil
}
