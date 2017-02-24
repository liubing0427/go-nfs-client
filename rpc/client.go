package rpc

import (
	"bufio"
	"bytes"
	"fmt"
	"math/rand"
	"net"
	"sync/atomic"
	"time"

	"github.com/fdawg4l/nfs/xdr"
)

const (
	MsgAccepted = iota
	MsgDenied
)

const (
	Success = iota
	ProgUnavail
	ProgMismatch
)

const (
	RpcMismatch = iota
)

var xid uint32

func init() {
	// seed the XID (which is set by the client)
	xid = rand.New(rand.NewSource(time.Now().UnixNano())).Uint32()
}

type Client struct {
	transport
}

func DialTCP(network string, ldr *net.TCPAddr, addr string) (*Client, error) {
	a, err := net.ResolveTCPAddr(network, addr)
	if err != nil {
		return nil, err
	}

	conn, err := net.DialTCP(a.Network(), ldr, a)
	if err != nil {
		return nil, err
	}

	t := &tcpTransport{
		Reader:      bufio.NewReader(conn),
		WriteCloser: conn,
	}

	return &Client{t}, nil
}

func (c *Client) Call(call interface{}) ([]byte, error) {
	msg := &message{
		Xid:     atomic.AddUint32(&xid, 1),
		Msgtype: 0,
		Body:    call,
	}

	w := new(bytes.Buffer)
	if err := xdr.Write(w, msg); err != nil {
		return nil, err
	}

	if err := c.send(w.Bytes()); err != nil {
		return nil, err
	}

	buf, err := c.recv()
	if err != nil {
		return nil, err
	}

	xid, buf := xdr.Uint32(buf)
	if xid != msg.Xid {
		return nil, fmt.Errorf("xid did not match, expected: %x, received: %x", msg.Xid, xid)
	}

	mtype, buf := xdr.Uint32(buf)
	if mtype != 1 {
		return nil, fmt.Errorf("message as not a reply: %d", mtype)
	}

	reply_stat, buf := xdr.Uint32(buf)
	switch reply_stat {
	case MsgAccepted:
		_, buf = xdr.Uint32(buf)
		opaque_len, buf := xdr.Uint32(buf)
		_ = buf[0:int(opaque_len)]
		buf = buf[opaque_len:]
		accept_stat, buf := xdr.Uint32(buf)

		switch accept_stat {
		case Success:
			return buf, nil
		case ProgUnavail:
			return nil, fmt.Errorf("PROG_UNAVAIL")
		case ProgMismatch:
			// TODO(dfc) decode mismatch_info
			return nil, fmt.Errorf("rpc: PROG_MISMATCH")
		default:
			return nil, fmt.Errorf("rpc: %d", accept_stat)
		}

	case MsgDenied:
		rejected_stat, _ := xdr.Uint32(buf)
		switch rejected_stat {
		case RpcMismatch:

		default:
			return nil, fmt.Errorf("rejected_stat was not valid: %d", rejected_stat)
		}

	default:
		return nil, fmt.Errorf("reply_stat was not valid: %d", reply_stat)
	}

	panic("unreachable")
}
