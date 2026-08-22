package history

import "fmt"

type TokenType int

const (
	TokenTypeUser = iota
	TokenTypeAssistent
	TokenTypeReasoning
	TokenTypeToolCall
	TokenTypeToolResult
	TokenTypeImage
	TokenEndOfSequence
)

type Token struct {
	Type    TokenType
	Content []string
}

func (token *Token) String() string {
	switch token.Type {
	case TokenTypeAssistent, TokenTypeReasoning, TokenTypeUser:
		return token.Content[0]
	case TokenTypeToolCall:
		return fmt.Sprintf("Calling tool [%s] with arguments %s...", token.Content[0], token.Content[1])
	case TokenTypeToolResult:
		return fmt.Sprintf("Recieved tool result: %s", token.Content[0])
	case TokenTypeImage: 
		return fmt.Sprintf("[Appended image, base64 size %.2fKB]", float64(len(token.Content[0])) / 1024)
	}
	return ""
}


func (token Token) Copy() Token {
	clone := token
	clone.Content = append([]string(nil), token.Content...)
	return clone
}
