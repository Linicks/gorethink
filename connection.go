package gorethink

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	p "github.com/dancannon/gorethink/ql2"
)

type Response struct {
	Token     int64
	Type      p.Response_ResponseType `json:"t"`
	Responses []interface{}           `json:"r"`
	Backtrace []interface{}           `json:"b"`
	Profile   interface{}             `json:"p"`
}

type Conn interface {
	SendQuery(s *Session, q *p.Query, t Term, opts map[string]interface{}, async bool) (*Cursor, error)
	ReadResponse(s *Session, token int64) (*Response, error)
	Close() error
}

// connection is a connection to a rethinkdb database
type Connection struct {
	// embed the net.Conn type, so that we can effectively define new methods on
	// it (interfaces do not allow that)
	net.Conn
	s *Session

	sync.Mutex
	closed bool
}

// Dial closes the previous connection and attempts to connect again.
func Dial(s *Session) (*Connection, error) {
	conn, err := net.Dial("tcp", s.address)
	if err != nil {
		return nil, RqlConnectionError{err.Error()}
	}

	// Send the protocol version to the server as a 4-byte little-endian-encoded integer
	if err := binary.Write(conn, binary.LittleEndian, p.VersionDummy_V0_3); err != nil {
		return nil, RqlConnectionError{err.Error()}
	}

	// Send the length of the auth key to the server as a 4-byte little-endian-encoded integer
	if err := binary.Write(conn, binary.LittleEndian, uint32(len(s.authkey))); err != nil {
		return nil, RqlConnectionError{err.Error()}
	}

	// Send the auth key as an ASCII string
	// If there is no auth key, skip this step
	if s.authkey != "" {
		if _, err := io.WriteString(conn, s.authkey); err != nil {
			return nil, RqlConnectionError{err.Error()}
		}
	}

	// Send the protocol type as a 4-byte little-endian-encoded integer
	if err := binary.Write(conn, binary.LittleEndian, p.VersionDummy_JSON); err != nil {
		return nil, RqlConnectionError{err.Error()}
	}

	// read server response to authorization key (terminated by NUL)
	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\x00')
	if err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("Unexpected EOF: %s", string(line))
		}
		return nil, RqlDriverError{err.Error()}
	}
	// convert to string and remove trailing NUL byte
	response := string(line[:len(line)-1])
	if response != "SUCCESS" {
		// we failed authorization or something else terrible happened
		return nil, RqlDriverError{fmt.Sprintf("Server dropped connection with message: \"%s\"", response)}
	}

	return &Connection{
		s:    s,
		Conn: conn,
	}, nil
}

func TestOnBorrow(c *Connection, t time.Time) error {
	c.SetReadDeadline(t)

	data := make([]byte, 1)
	if _, err := c.Read(data); err != nil {
		e, ok := err.(net.Error)
		if err != nil && !(ok && e.Timeout()) {
			return err
		}
	}

	c.SetReadDeadline(time.Time{})
	return nil
}

func (c *Connection) ReadResponse(s *Session, token int64) (*Response, error) {
	for {
		// Read the 8-byte token of the query the response corresponds to.
		var responseToken int64
		if err := binary.Read(c, binary.LittleEndian, &responseToken); err != nil {
			c.Close()
			return nil, RqlConnectionError{err.Error()}
		}

		// Read the length of the JSON-encoded response as a 4-byte
		// little-endian-encoded integer.
		var messageLength int64
		if err := binary.Read(c, binary.LittleEndian, &messageLength); err != nil {
			c.Close()
			return nil, RqlConnectionError{err.Error()}
		}

		// Read the JSON encoding of the Response itself.
		b := make([]byte, messageLength)
		if _, err := io.ReadFull(c, b); err != nil {
			c.Close()
			return nil, RqlDriverError{err.Error()}
		}

		var response *Response
		response.Token = responseToken
		err := json.Unmarshal(b, &response)
		if err != nil {
			return nil, RqlDriverError{err.Error()}
		}

		if responseToken == token {
			return response, nil
		} else if cursor, ok := s.checkCache(token); ok {
			// Handle batch response
			s.handleBatchResponse(cursor, response)
		} else {
			return nil, RqlDriverError{"Unexpected response received"}
		}
	}
}

func (c *Connection) SendQuery(s *Session, q *p.Query, t Term, opts map[string]interface{}, async bool) (*Cursor, error) {
	var err error

	// Build query
	b, err := json.Marshal([]interface{}{q.GetType(), t, opts})
	if err != nil {
		return nil, RqlDriverError{"Error building query"}
	}

	// Ensure that the connection is not closed
	if s.closed {
		return nil, RqlDriverError{"Connection is closed"}
	}

	// Set timeout
	if s.timeout == 0 {
		c.SetDeadline(time.Time{})
	} else {
		c.SetDeadline(time.Now().Add(s.timeout))
	}

	// Send a unique 8-byte token
	if err = binary.Write(c, binary.LittleEndian, q.GetToken()); err != nil {
		c.Close()
		return nil, RqlConnectionError{err.Error()}
	}

	// Send the length of the JSON-encoded query as a 4-byte
	// little-endian-encoded integer.
	if err = binary.Write(c, binary.LittleEndian, len(b)); err != nil {
		c.Close()
		return nil, RqlConnectionError{err.Error()}
	}

	// Send the JSON encoding of the query itself.
	if err = binary.Write(c, binary.BigEndian, b); err != nil {
		c.Close()
		return nil, RqlConnectionError{err.Error()}
	}

	// Return immediately if the noreply option was set
	if noreply, ok := opts["noreply"]; ok && noreply.(bool) {
		c.Close()
		return nil, nil
	} else if async {
		return nil, nil
	}

	// Get response
	response, err := c.ReadResponse(s, *q.Token)
	if err != nil {
		return nil, err
	}

	err = checkErrorResponse(response, t)
	if err != nil {
		return nil, err
	}

	// De-construct datum and return a cursor
	switch response.Type {
	case p.Response_SUCCESS_PARTIAL, p.Response_SUCCESS_SEQUENCE, p.Response_SUCCESS_FEED:
		cursor := &Cursor{
			session: s,
			conn:    c,
			query:   q,
			term:    t,
			opts:    opts,
			profile: response.Profile,
		}

		s.setCache(*q.Token, cursor)

		cursor.extend(response)

		return cursor, nil
	case p.Response_SUCCESS_ATOM:
		var value []interface{}
		var err error

		if len(response.Responses) < 1 {
			value = []interface{}{}
		} else {
			var v interface{}

			v, err = recursivelyConvertPseudotype(response.Responses[0], opts)
			if err != nil {
				return nil, err
			}
			if err != nil {
				return nil, RqlDriverError{err.Error()}
			}

			if sv, ok := v.([]interface{}); ok {
				value = sv
			} else if v == nil {
				value = []interface{}{nil}
			} else {
				value = []interface{}{v}
			}
		}

		cursor := &Cursor{
			session:  s,
			conn:     c,
			query:    q,
			term:     t,
			opts:     opts,
			profile:  response.Profile,
			buffer:   value,
			finished: true,
		}

		return cursor, nil
	case p.Response_WAIT_COMPLETE:
		return nil, nil
	default:
		return nil, RqlDriverError{fmt.Sprintf("Unexpected response type received: %s", response.Type)}
	}
}

func (c *Connection) Close() error {
	err := c.s.noreplyWaitQuery()
	if err != nil {
		return err
	}

	return c.CloseNoWait()
}

func (c *Connection) CloseNoWait() error {
	c.Lock()
	c.closed = true
	c.Unlock()

	return c.Conn.Close()
}

func checkErrorResponse(response *Response, t Term) error {
	switch response.Type {
	case p.Response_CLIENT_ERROR:
		return RqlClientError{rqlResponseError{response, t}}
	case p.Response_COMPILE_ERROR:
		return RqlCompileError{rqlResponseError{response, t}}
	case p.Response_RUNTIME_ERROR:
		return RqlRuntimeError{rqlResponseError{response, t}}
	}

	return nil
}
