// Package json implements the "@std/json" Luau library.
package json

import (
	stdjson "encoding/json"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Leoj030/go-luau/luau"
)

func Module() map[string]luau.Func {
	return map[string]luau.Func{
		"deserialize": deserialize,
		"serialize":   serialize,
	}
}

func deserialize(c *luau.State) (int, error) {
	input, err := stringArg(c, "deserialize")
	if err != nil {
		return 0, err
	}

	if err := decodeToLuau(c, input); err != nil {
		return 0, fmt.Errorf("json.deserialize: %w", err)
	}
	return 1, nil
}

func serialize(c *luau.State) (int, error) {
	if c.Top() < 1 || c.Top() > 2 {
		return 0, fmt.Errorf("json.serialize: expected 1 or 2 arguments, got %d", c.Top())
	}
	value, err := c.Get(1)
	if err != nil {
		return 0, fmt.Errorf("json.serialize: %w", err)
	}

	pretty := false
	if c.Top() == 2 {
		if c.TypeName(2) != "boolean" {
			return 0, fmt.Errorf("json.serialize: argument 2 must be a boolean")
		}
		formatValue, err := c.Get(2)
		if err != nil {
			return 0, fmt.Errorf("json.serialize: %w", err)
		}
		var ok bool
		pretty, ok = formatValue.(bool)
		if !ok {
			return 0, fmt.Errorf("json.serialize: argument 2 must be a boolean")
		}
	}

	encoded, err := marshal(value, pretty)
	if err != nil {
		return 0, fmt.Errorf("json.serialize: %w", err)
	}
	if err := c.Push(string(encoded)); err != nil {
		return 0, fmt.Errorf("json.serialize: %w", err)
	}
	return 1, nil
}

func stringArg(c *luau.State, fn string) (string, error) {
	if c.Top() != 1 {
		return "", fmt.Errorf("json.%s: expected 1 argument, got %d", fn, c.Top())
	}
	value, err := c.Get(1)
	if err != nil {
		return "", fmt.Errorf("json.%s: %w", fn, err)
	}
	input, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("json.%s: argument 1 must be a string", fn)
	}
	return input, nil
}

func unmarshal(input string) (any, error) {
	data := []byte(input)
	var value any
	if err := jsonv2.Unmarshal(data, &value,
		jsontext.AllowDuplicateNames(true),
	); err != nil {
		if err := stdjson.Unmarshal(data, &value); err != nil {
			return nil, err
		}
	}
	return value, nil
}

const maxJSONDepth = 100

func decodeToLuau(c *luau.State, input string) error {
	base := c.Top()
	decoder := jsontext.NewDecoder(strings.NewReader(input),
		jsontext.AllowDuplicateNames(true),
		jsontext.AllowInvalidUTF8(true),
	)

	token, err := decoder.ReadToken()
	if err == nil {
		err = pushToken(c, decoder, token, 0)
	}
	if err == nil {
		_, err = decoder.ReadToken()
		switch err {
		case nil:
			err = fmt.Errorf("unexpected data after JSON value")
		case io.EOF:
			err = nil
		}
	}
	if err == nil {
		return nil
	}

	if popErr := c.Pop(c.Top() - base); popErr != nil {
		return fmt.Errorf("%w (stack cleanup failed: %v)", err, popErr)
	}
	_, legacyErr := unmarshal(input)
	if legacyErr != nil {
		return legacyErr
	}
	return err
}

func pushToken(c *luau.State, decoder *jsontext.Decoder, token jsontext.Token, depth int) error {
	switch token.Kind() {
	case jsontext.KindNull:
		return c.Push(nil)
	case jsontext.KindFalse:
		return c.Push(false)
	case jsontext.KindTrue:
		return c.Push(true)
	case jsontext.KindString:
		return c.Push(token.String())
	case jsontext.KindNumber:
		value, err := token.Float()
		if err != nil {
			return err
		}
		return c.Push(value)
	case jsontext.KindBeginArray:
		if depth >= maxJSONDepth {
			return fmt.Errorf("luau: value is nested more than %d levels deep", maxJSONDepth)
		}
		if err := c.NewTable(0, 0); err != nil {
			return err
		}
		for index := 1; ; index++ {
			next, err := decoder.ReadToken()
			if err != nil {
				return err
			}
			if next.Kind() == jsontext.KindEndArray {
				return nil
			}
			if err := pushToken(c, decoder, next, depth+1); err != nil {
				return err
			}
			if err := c.SetIndex(-2, index); err != nil {
				return err
			}
		}
	case jsontext.KindBeginObject:
		if depth >= maxJSONDepth {
			return fmt.Errorf("luau: value is nested more than %d levels deep", maxJSONDepth)
		}
		if err := c.NewTable(0, 0); err != nil {
			return err
		}
		for {
			key, err := decoder.ReadToken()
			if err != nil {
				return err
			}
			if key.Kind() == jsontext.KindEndObject {
				return nil
			}
			if key.Kind() != jsontext.KindString {
				return fmt.Errorf("expected JSON object name, got %s", key.Kind())
			}
			name := key.String()
			value, err := decoder.ReadToken()
			if err != nil {
				return err
			}
			if err := c.Push(name); err != nil {
				return err
			}
			if err := pushToken(c, decoder, value, depth+1); err != nil {
				return err
			}
			if err := c.RawSet(-3); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unexpected JSON token %s", token.Kind())
	}
}

func marshal(value any, pretty bool) ([]byte, error) {
	return appendValue(nil, value, pretty, 0)
}

func appendValue(dst []byte, value any, pretty bool, depth int) ([]byte, error) {
	switch v := value.(type) {
	case nil:
		return append(dst, "null"...), nil
	case bool:
		return strconv.AppendBool(dst, v), nil
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, marshalFallback(value)
		}
		format := byte('f')
		if v != 0 && (math.Abs(v) < 1e-6 || math.Abs(v) >= 1e21) {
			format = 'e'
		}
		return appendFloat(dst, v, format), nil
	case string:
		return appendString(dst, v), nil
	case []any:
		dst = append(dst, '[')
		for i, item := range v {
			if i > 0 {
				dst = append(dst, ',')
			}
			if pretty {
				dst = append(dst, '\n')
				dst = appendIndent(dst, depth+1)
			}
			var err error
			dst, err = appendValue(dst, item, pretty, depth+1)
			if err != nil {
				return nil, err
			}
		}
		if pretty && len(v) > 0 {
			dst = append(dst, '\n')
			dst = appendIndent(dst, depth)
		}
		return append(dst, ']'), nil
	case map[string]any:
		dst = append(dst, '{')
		if len(v) == 0 {
			return append(dst, '}'), nil
		}
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for i, key := range keys {
			if i > 0 {
				dst = append(dst, ',')
			}
			if pretty {
				dst = append(dst, '\n')
				dst = appendIndent(dst, depth+1)
			}
			dst = appendString(dst, key)
			if pretty {
				dst = append(dst, ':', ' ')
			} else {
				dst = append(dst, ':')
			}
			var err error
			dst, err = appendValue(dst, v[key], pretty, depth+1)
			if err != nil {
				return nil, err
			}
		}
		if pretty {
			dst = append(dst, '\n')
			dst = appendIndent(dst, depth)
		}
		return append(dst, '}'), nil
	default:
		encoded, err := stdjson.Marshal(value)
		if err != nil {
			return nil, err
		}
		return append(dst, encoded...), nil
	}
}

func marshalFallback(value any) error {
	_, err := stdjson.Marshal(value)
	return err
}

func appendIndent(dst []byte, depth int) []byte {
	for range depth {
		dst = append(dst, ' ', ' ')
	}
	return dst
}

func appendFloat(dst []byte, value float64, format byte) []byte {
	start := len(dst)
	dst = strconv.AppendFloat(dst, value, format, -1, 64)
	for i := start; i < len(dst); i++ {
		if dst[i] == 'e' {
			exponent := i + 1
			if dst[exponent] == '+' || dst[exponent] == '-' {
				exponent++
			}
			if exponent+1 < len(dst) && dst[exponent] == '0' {
				copy(dst[exponent:], dst[exponent+1:])
				dst = dst[:len(dst)-1]
			}
			break
		}
	}
	return dst
}

func appendString(dst []byte, value string) []byte {
	dst = append(dst, '"')
	for i := 0; i < len(value); {
		b := value[i]
		if b < utf8.RuneSelf {
			switch b {
			case '"', '\\':
				dst = append(dst, '\\', b)
			case '\b':
				dst = append(dst, '\\', 'b')
			case '\f':
				dst = append(dst, '\\', 'f')
			case '\n':
				dst = append(dst, '\\', 'n')
			case '\r':
				dst = append(dst, '\\', 'r')
			case '\t':
				dst = append(dst, '\\', 't')
			case '<':
				dst = append(dst, `\u003c`...)
			case '>':
				dst = append(dst, `\u003e`...)
			case '&':
				dst = append(dst, `\u0026`...)
			default:
				if b < 0x20 {
					dst = append(dst, `\u00`...)
					dst = append(dst, "0123456789abcdef"[b>>4], "0123456789abcdef"[b&0x0f])
				} else {
					dst = append(dst, b)
				}
			}
			i++
			continue
		}

		r, size := utf8.DecodeRuneInString(value[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			dst = append(dst, "\uFFFD"...)
			i++
		case r == '\u2028':
			dst = append(dst, `\u2028`...)
			i += size
		case r == '\u2029':
			dst = append(dst, `\u2029`...)
			i += size
		default:
			dst = append(dst, value[i:i+size]...)
			i += size
		}
	}
	return append(dst, '"')
}
