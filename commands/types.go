package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// Error descriptor
type Error struct {
	Error string `json:"error"`
}

// ResponseJSON formatter
func ResponseJSON(res interface{}) {
	data, err := json.Marshal(res)
	if err != nil {
		ResponseError(err)
		return
	}
	buf := bytes.Buffer{}
	json.Indent(&buf, data, "", "\t")
	buf.WriteString("\n")
	buf.WriteTo(os.Stdout)
}

// ResponseError error handler
func ResponseError(res interface{}) {
	data, e := json.Marshal(res)
	if e != nil {
		fmt.Fprintf(os.Stderr, "%v (%v)\n", e, res)
		return
	}
	buf := bytes.Buffer{}
	json.Indent(&buf, data, "", "\t")
	buf.WriteString("\n")
	buf.WriteTo(os.Stderr)
}
