// Copyright (c) 2016-present Cloud <cloud@txthinking.com>
//
// This program is free software; you can redistribute it and/or
// modify it under the terms of version 3 of the GNU General Public
// License as published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful, but
// WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU
// General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.

package brook

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"time"

	"github.com/txthinking/socks5"
	"github.com/txthinking/x"
)

type SimpleStreamServer struct {
	Client  net.Conn
	Timeout int
	RB      []byte
	WB      []byte
	network string
	src     string
	dst     string
}

func NewSimpleStreamServer(password []byte, src string, client net.Conn, timeout, udptimeout int) (Exchanger, error) {
	if timeout != 0 {
		if err := client.SetDeadline(time.Now().Add(time.Duration(timeout) * time.Second)); err != nil {
			return nil, err
		}
	}
	s := &SimpleStreamServer{Client: client, Timeout: timeout, src: src}
	b := x.BP2048.Get().([]byte)
	if _, err := io.ReadFull(s.Client, b[:32+2]); err != nil {
		x.BP2048.Put(b)
		return nil, err
	}
	if bytes.Compare(password, b[:32]) != 0 {
		x.BP2048.Put(b)
		WaitReadErr(s.Client)
		return nil, errors.New("Password is wrong")
	}
	l := int(binary.BigEndian.Uint16(b[32:34]))
	if l > 2048 {
		x.BP2048.Put(b)
		return nil, errors.New("data too long")
	}
	if _, err := io.ReadFull(s.Client, b[:l]); err != nil {
		x.BP2048.Put(b)
		return nil, err
	}
	i := int64(binary.BigEndian.Uint32(b[:4]))
	if time.Now().Unix()-i > 60 {
		x.BP2048.Put(b)
		WaitReadErr(s.Client)
		return nil, errors.New("Expired request")
	}
	if i%2 == 0 {
		s.network = "tcp"
		s.RB = b
		s.WB = x.BP2048.Get().([]byte)
	}
	if i%2 == 1 {
		s.network = "udp"
		s.Timeout = udptimeout
		s.RB = x.BP65507.Get().([]byte)
		copy(s.RB[:l], b[:l])
		x.BP2048.Put(b)
		s.WB = x.BP65507.Get().([]byte)
	}
	s.dst = socks5.ToAddress(s.RB[4], s.RB[4+1:l-2], s.RB[l-2:])
	return ServerGate(s)
}

func (s *SimpleStreamServer) Exchange(remote net.Conn) error {
	go func() {
		if s.network == "tcp" && s.Timeout == 0 {
			io.Copy(s.Client, remote)
			return
		}
		for {
			if s.Timeout != 0 {
				if err := remote.SetDeadline(time.Now().Add(time.Duration(s.Timeout) * time.Second)); err != nil {
					return
				}
			}
			if s.network == "tcp" {
				l, err := remote.Read(s.WB)
				if err != nil {
					return
				}
				if _, err := s.Client.Write(s.WB[:l]); err != nil {
					return
				}
			}
			if s.network == "udp" {
				l, err := remote.Read(s.WB[2:])
				if err != nil {
					return
				}
				binary.BigEndian.PutUint16(s.WB[:2], uint16(l))
				if _, err := s.Client.Write(s.WB[:2+l]); err != nil {
					return
				}
			}
		}
	}()
	if s.network == "tcp" && s.Timeout == 0 {
		io.Copy(remote, s.Client)
		return nil
	}
	for {
		if s.Timeout != 0 {
			if err := s.Client.SetDeadline(time.Now().Add(time.Duration(s.Timeout) * time.Second)); err != nil {
				return nil
			}
		}
		if s.network == "tcp" {
			l, err := s.Client.Read(s.RB)
			if err != nil {
				return nil
			}
			if _, err := remote.Write(s.RB[:l]); err != nil {
				return nil
			}
		}
		if s.network == "udp" {
			if _, err := io.ReadFull(s.Client, s.RB[:2]); err != nil {
				return nil
			}
			l := int(binary.BigEndian.Uint16(s.RB[:2]))
			if l > 65507-2 {
				return errors.New("packet too long")
			}
			if _, err := io.ReadFull(s.Client, s.RB[2:2+l]); err != nil {
				return nil
			}
			if _, err := remote.Write(s.RB[2 : 2+l]); err != nil {
				return nil
			}
		}
	}
	return nil
}

func (s *SimpleStreamServer) Network() string {
	return s.network
}

func (s *SimpleStreamServer) Src() string {
	return s.src
}

func (s *SimpleStreamServer) Dst() string {
	return s.dst
}

func (s *SimpleStreamServer) Clean() {
	if s.network == "tcp" {
		x.BP2048.Put(s.WB)
		x.BP2048.Put(s.RB)
	}
	if s.network == "udp" {
		x.BP65507.Put(s.WB)
		x.BP65507.Put(s.RB)
	}
}
