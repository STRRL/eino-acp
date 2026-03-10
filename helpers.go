package einoacp

import "github.com/cloudwego/eino/schema"

// UserMessages is a convenience function to create a single user message slice.
func UserMessages(content string) []*schema.Message {
	return []*schema.Message{
		{Role: schema.User, Content: content},
	}
}
