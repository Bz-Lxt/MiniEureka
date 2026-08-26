package gossip

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

type DatagramHandler func(context.Context, []byte, *net.UDPAddr)

type UDPTransport struct {
	address  string
	maxBytes int
	faults   *Faults
	mu       sync.Mutex
	conn     *net.UDPConn
	wg       sync.WaitGroup
	closed   bool
}

func NewUDPTransport(address string, maxBytes int, faults *Faults) *UDPTransport {
	if maxBytes <= 0 || maxBytes > 1200 {
		maxBytes = 1200
	}
	return &UDPTransport{address: address, maxBytes: maxBytes, faults: faults}
}

func (t *UDPTransport) Listen(ctx context.Context, handler DatagramHandler) error {
	address, err := net.ResolveUDPAddr("udp", t.address)
	if err != nil {
		return fmt.Errorf("resolve gossip listen address: %w", err)
	}
	conn, err := net.ListenUDP("udp", address)
	if err != nil {
		return fmt.Errorf("listen gossip udp: %w", err)
	}
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		_ = conn.Close()
		return net.ErrClosed
	}
	t.conn = conn
	t.mu.Unlock()
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		<-ctx.Done()
		_ = t.Close()
	}()
	buffer := make([]byte, t.maxBytes+1)
	for {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		read, remote, readErr := conn.ReadFromUDP(buffer)
		if readErr != nil {
			if errors.Is(readErr, net.ErrClosed) || ctx.Err() != nil {
				return nil
			}
			if timeout, ok := readErr.(net.Error); ok && timeout.Timeout() {
				continue
			}
			return fmt.Errorf("read gossip udp: %w", readErr)
		}
		if read > t.maxBytes {
			continue
		}
		packet := append([]byte(nil), buffer[:read]...)
		handler(ctx, packet, remote)
	}
}

func (t *UDPTransport) Send(ctx context.Context, peerNodeID, address string, data []byte) error {
	if len(data) == 0 || len(data) > t.maxBytes {
		return ErrMessageTooLarge
	}
	if t.faults != nil && t.faults.ShouldDrop(peerNodeID) {
		return nil
	}
	remote, err := net.ResolveUDPAddr("udp", address)
	if err != nil {
		return fmt.Errorf("resolve gossip peer: %w", err)
	}
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()
	if conn == nil {
		return errors.New("gossip transport is not listening")
	}
	deadline := time.Now().Add(time.Second)
	if callerDeadline, ok := ctx.Deadline(); ok && callerDeadline.Before(deadline) {
		deadline = callerDeadline
	}
	if err := conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	_, err = conn.WriteToUDP(data, remote)
	return err
}

func (t *UDPTransport) LocalAddr() *net.UDPAddr {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn == nil {
		return nil
	}
	address, _ := t.conn.LocalAddr().(*net.UDPAddr)
	return address
}

func (t *UDPTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	conn := t.conn
	t.mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func (t *UDPTransport) Wait() {
	t.wg.Wait()
}
