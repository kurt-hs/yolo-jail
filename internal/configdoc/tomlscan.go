package configdoc

func lineStart(data []byte, offset int) int {
	if offset > len(data) {
		offset = len(data)
	}
	for offset > 0 && data[offset-1] != '\n' {
		offset--
	}
	return offset
}

func lineEnd(data []byte, offset int) int {
	for offset < len(data) && data[offset] != '\n' && data[offset] != '\r' {
		offset++
	}
	return offset
}

func skipTOMLSpace(data []byte, offset int) int {
	for offset < len(data) && (data[offset] == ' ' || data[offset] == '\t' || data[offset] == '\r' || data[offset] == '\n') {
		offset++
	}
	return offset
}

// scanTOMLValue returns the syntactic value extent beginning at offset.
func scanTOMLValue(data []byte, offset int) Span {
	start := skipTOMLSpace(data, offset)
	if start >= len(data) {
		return Span{start, start}
	}
	if data[start] == '#' {
		return Span{start, start}
	}
	if data[start] == '\'' || data[start] == '"' {
		return Span{start, scanTOMLString(data, start)}
	}
	if data[start] == '[' || data[start] == '{' {
		stack := []byte{data[start]}
		quote := byte(0)
		multiline := false
		escaped := false
		for i := start + 1; i < len(data); i++ {
			c := data[i]
			if quote != 0 {
				if quote == '"' && escaped {
					escaped = false
					continue
				}
				if quote == '"' && c == '\\' && !multiline {
					escaped = true
					continue
				}
				if multiline && i+2 < len(data) && data[i] == quote && data[i+1] == quote && data[i+2] == quote {
					quote = 0
					multiline = false
					i += 2
					continue
				}
				if !multiline && c == quote {
					quote = 0
				}
				continue
			}
			if c == '\'' || c == '"' {
				quote = c
				if i+2 < len(data) && data[i+1] == c && data[i+2] == c {
					multiline = true
					i += 2
				}
				continue
			}
			if c == '#' {
				for i < len(data) && data[i] != '\n' {
					i++
				}
				continue
			}
			if c == '[' || c == '{' {
				stack = append(stack, c)
				continue
			}
			if c == ']' || c == '}' {
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
				if len(stack) == 0 {
					return Span{start, i + 1}
				}
			}
		}
		return Span{start, len(data)}
	}
	end := start
	for end < len(data) && data[end] != ' ' && data[end] != '\t' && data[end] != '\r' && data[end] != '\n' && data[end] != ',' && data[end] != ']' && data[end] != '}' && data[end] != '#' {
		end++
	}
	return Span{start, end}
}

func scanTOMLString(data []byte, start int) int {
	quote := data[start]
	multiline := start+2 < len(data) && data[start+1] == quote && data[start+2] == quote
	pos := start + 1
	if multiline {
		pos += 2
	}
	for pos < len(data) {
		if quote == '"' && data[pos] == '\\' {
			pos += 2
			continue
		}
		if multiline && pos+2 < len(data) && data[pos] == quote && data[pos+1] == quote && data[pos+2] == quote {
			return pos + 3
		}
		if !multiline && data[pos] == quote {
			return pos + 1
		}
		pos++
	}
	return len(data)
}

func tomlComments(data []byte) []Span {
	var out []Span
	for i := 0; i < len(data); {
		if data[i] == '\'' || data[i] == '"' {
			i = scanTOMLString(data, i)
			continue
		}
		if data[i] == '#' {
			start := i
			for i < len(data) && data[i] != '\n' && data[i] != '\r' {
				i++
			}
			out = append(out, Span{start, i})
			continue
		}
		i++
	}
	return out
}
