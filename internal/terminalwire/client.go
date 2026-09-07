package terminalwire

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/vmihailenco/msgpack/v5"
)

const ProtocolVersion = "0.3.4"

// Message is the wire format for Terminalwire protocol (MessagePack-encoded maps).
type Message map[string]interface{}

// Client implements the Terminalwire client protocol over WebSocket.
type Client struct {
	conn           *websocket.Conn
	authority      string
	storagePath    string
	programName    string
	programVersion string
	stdinReader    *bufio.Reader
}

// Connect establishes a WebSocket connection to a Terminalwire server.
func Connect(wsURL, programName, programVersion string) (*Client, error) {
	u, err := url.Parse(wsURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}

	authority := u.Host
	homeDir, _ := os.UserHomeDir()
	storagePath := filepath.Join(homeDir, ".micepad", "authorities", authority, "storage")

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("websocket connect: %w", err)
	}

	return &Client{
		conn:           conn,
		authority:      authority,
		storagePath:    storagePath,
		programName:    programName,
		programVersion: programVersion,
		stdinReader:    bufio.NewReader(os.Stdin),
	}, nil
}

// Run sends the initialization message and enters the main event loop.
func (c *Client) Run(args []string) error {
	defer c.conn.Close()

	// Copy local file arguments to storage so the server can access them.
	// This handles the case where users pass file paths directly
	// (e.g., ~/Downloads/attendees.csv) instead of copying to storage first.
	args = c.prepareFileArgs(args)

	if err := c.sendInit(args); err != nil {
		return fmt.Errorf("init: %w", err)
	}

	for {
		msg, err := c.readMsg()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		if done, err := c.dispatch(msg); done {
			return nil
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "handle error: %v\n", err)
		}
	}
}

// prepareFileArgs copies local file arguments into the storage directory so the
// server can access them via the Terminalwire file resource. For each non-flag
// argument that resolves to an existing file outside of storage, the file is
// copied and the argument is replaced with just the filename.
func (c *Client) prepareFileArgs(args []string) []string {
	result := make([]string, len(args))
	copy(result, args)

	for i, arg := range result {
		if strings.HasPrefix(arg, "-") {
			continue
		}

		localPath := arg
		if strings.HasPrefix(localPath, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				continue
			}
			localPath = filepath.Join(home, localPath[2:])
		}
		absPath, err := filepath.Abs(localPath)
		if err != nil {
			continue
		}

		info, err := os.Stat(absPath)
		if err != nil || info.IsDir() || info.Size() > 10*1024*1024 {
			continue
		}

		if strings.HasPrefix(absPath, c.storagePath) {
			continue
		}

		if err := os.MkdirAll(c.storagePath, 0755); err != nil {
			continue
		}

		data, err := os.ReadFile(absPath)
		if err != nil {
			continue
		}

		destPath := filepath.Join(c.storagePath, filepath.Base(absPath))
		if err := os.WriteFile(destPath, data, 0644); err != nil {
			continue
		}

		result[i] = filepath.Base(absPath)
	}

	return result
}

func (c *Client) sendInit(args []string) error {
	homeDir, _ := os.UserHomeDir()
	storagePattern := filepath.Join(homeDir, ".micepad", "authorities", c.authority, "storage", "**/*")

	return c.writeMsg(Message{
		"event": "initialization",
		"protocol": map[string]interface{}{
			"version": ProtocolVersion,
		},
		"entitlement": map[string]interface{}{
			"authority":             c.authority,
			"schemes":               []interface{}{"http", "https"},
			"paths":                 []interface{}{c.storagePath, storagePattern},
			"environment_variables": []interface{}{"TERMINALWIRE_HOME"},
		},
		"program": map[string]interface{}{
			"name":      c.programName,
			"version":   c.programVersion,
			"arguments": args,
		},
	})
}

func (c *Client) dispatch(msg Message) (done bool, err error) {
	event, _ := msg["event"].(string)

	switch event {
	case "resource":
		return false, c.handleResource(msg)
	case "exit":
		exitWithStatus(msg["status"])
		return true, nil
	default:
		return false, fmt.Errorf("unknown event: %s", event)
	}
}

func (c *Client) writeMsg(msg Message) error {
	data, err := msgpack.Marshal(msg)
	if err != nil {
		return fmt.Errorf("msgpack encode: %w", err)
	}
	return c.conn.WriteMessage(websocket.BinaryMessage, data)
}

func (c *Client) readMsg() (Message, error) {
	_, data, err := c.conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("websocket read: %w", err)
	}

	var msg Message
	if err := msgpack.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("msgpack decode: %w", err)
	}
	return msg, nil
}

func (c *Client) succeed(name string, response interface{}) error {
	return c.writeMsg(Message{
		"event":    "resource",
		"name":     name,
		"status":   "success",
		"response": response,
	})
}

func (c *Client) fail(name string, reason string) error {
	return c.writeMsg(Message{
		"event":    "resource",
		"name":     name,
		"status":   "failure",
		"response": reason,
	})
}

func exitWithStatus(status interface{}) {
	code := 0
	switch v := status.(type) {
	case int64:
		code = int(v)
	case uint64:
		code = int(v)
	case int8:
		code = int(v)
	case uint8:
		code = int(v)
	case float64:
		code = int(v)
	}
	os.Exit(code)
}
