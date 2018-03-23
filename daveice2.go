//
//   Copyright 2016, Global Radio Ltd.
//
//   Licensed under the Apache License, Version 2.0 (the "License");
//   you may not use this file except in compliance with the License.
//   You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.
//

package main

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"time"

	"adts"
	"icecast"
)

const DAVECAST_DATA = 0
const DAVECAST_METADATA = 1
const DAVECAST_ANNOUNCE = 2
const DAVECAST_HEADERS = 3

const AAC_2C_44100_48000 = 0
const MP3_2C_44100_128000 = 1
const AAC_2C_44100_192000 = 2
const AAC_2C_44100_128000 = 3
const MP3_1C_44100_48000 = 4
const AAC_2C_44100_24000 = 5

type nanosec int64
type davecast struct {
	time       nanosec
	mtype      int
	replica    int
	uuid       []byte
	seq        uint64
	data       []byte
	mountpoint string
	metadata   string
	atype      int
	headers    string
}

var relays []chan []byte
var seq uint64 = 0

func main() {
	server := os.Args[1]
	stream := os.Args[2]

	for n := 3; n < len(os.Args); n++ {
		r := make(chan []byte, 100)
		relays = append(relays, r)
		if strings.Contains(os.Args[n], "@") {
			go udp_client(strings.Replace(os.Args[n], "@", ":", 1), r)
		} else {
			go tcp_client(os.Args[n], r)
		}
	}

	dc := make(chan davecast, 10)
	defer close(dc)

	go relay_pdu(dc)
	http_client(server, stream, dc)
}

func relay_pdu(dc chan davecast) {
	defer func() {
		for _, r := range relays {
			close(r)
		}
	}()

	for {
		select {
		case <-time.After(time.Second * 30):
			log.Println("timeout")
			return

		case pdu, ok := <-dc:
			if !ok {
				return
			}

			pdu.seq = seq
			seq++

			for n, r := range relays {
				pdu.replica = n
				select {
				case r <- pdu_to_bytes(pdu):
				default:
				}
			}
		}
	}
}

func udp_client(addr string, messages chan []byte) {

	defer func() {
		time.Sleep(3000 * time.Millisecond)
		go udp_client(addr, messages)
	}()

	// connect to this socket
	if conn, err := net.Dial("udp", addr); err != nil {
		return
	} else {
		defer func() {
			//log.Printf("udp closing: %s\n", addr);
			conn.Close()
		}()

		log.Printf("udp opened: %s\n", addr)

		for {
			buff := <-messages
			if _, err := conn.Write(buff); err != nil {
				log.Println("Error writing:", err.Error())
				return
			}
		}
	}
}

func tcp_client(addr string, messages chan []byte) {

	defer func() {
		time.Sleep(3000 * time.Millisecond)
		go tcp_client(addr, messages)
	}()

	// connect to this socket
	if conn, err := net.Dial("tcp", addr); err != nil {
		return
	} else {
		defer func() {
			log.Printf("tcp closing: %s\n", addr)
			conn.Close()
		}()

		log.Printf("tcp opened: %s\n", addr)

		for {
			buff := <-messages
			out := make([]byte, len(buff)+2)
			out[0] = byte(len(buff) >> 8)
			out[1] = byte(len(buff) % 256)
			copy(out[2:], buff[:])
			if n, err := conn.Write(out); err != nil || n != len(out) {
				log.Printf("Error writing: %s", err.Error())
				return
			}
		}
	}
}

func pdu_to_bytes(pdu davecast) []byte {
	size := 26
	seqn := make([]byte, 8)
	binary.BigEndian.PutUint64(seqn, uint64(pdu.seq))

	switch pdu.mtype {
	case DAVECAST_DATA:
		size += len(pdu.data)
	case DAVECAST_METADATA:
		size += len(pdu.metadata)
	case DAVECAST_ANNOUNCE:
		size += (1 + len(pdu.mountpoint))
	case DAVECAST_HEADERS:
		size += len(pdu.headers)
	}

	buff := make([]byte, size)
	buff[0] = byte(pdu.mtype)
	buff[1] = byte(pdu.replica)
	copy(buff[2:], pdu.uuid[0:16])
	copy(buff[18:], seqn[:])

	switch pdu.mtype {
	case DAVECAST_DATA:
		copy(buff[26:], pdu.data[:])

	case DAVECAST_METADATA:
		copy(buff[26:], pdu.metadata[:])

	case DAVECAST_ANNOUNCE:
		buff[26] = byte(pdu.atype)
		copy(buff[27:], []byte(pdu.mountpoint))

	case DAVECAST_HEADERS:
		copy(buff[26:], pdu.headers[:])
	}

	return buff
}

// RFC 4122
func new_uuid() []byte {
	uuid := make([]byte, 16)
	n, err := io.ReadFull(rand.Reader, uuid)
	if n != len(uuid) || err != nil {
		panic("unable to read random data")
	}
	uuid[8] = uuid[8]&^0xc0 | 0x80 // variants 4.1.1
	uuid[6] = uuid[6]&^0xf0 | 0x40 // v4 4.1.3
	return uuid[:]
}

func http_client(server string, mountpoint string, dc chan davecast) {
	var pdu davecast
	pdu.mountpoint = mountpoint
	pdu.atype = AAC_2C_44100_48000
	pdu.uuid = nil

	source := fmt.Sprintf("http://%s/%s", server, mountpoint)
	//parser := other_parser()
	parser := adts.RAW()	

	icecast.Open(source, func(buff []byte, is_meta bool, i icecast.Icecast) {
		if pdu.uuid == nil {
			pdu.uuid = new_uuid()

			mtype := "UNK"

			switch i.ContentType {
			case "audio/aac":
				mtype = "AAC"
				parser = adts.ADTS()

			case "audio/aacp":
				mtype = "AAC"
				parser = adts.ADTS()

			case "audio/mpeg":
				mtype = "MP3"
				parser = adts.MPEG()
			}

			ice_ainfo := fmt.Sprintf("%s_%dC_%d_%d000", mtype, i.Channels,
				i.SampleRate, i.BitRate)

			log.Println(ice_ainfo)

			switch ice_ainfo {
			case "AAC_2C_44100_48000":
				pdu.atype = AAC_2C_44100_48000

			case "MP3_2C_44100_128000":
				pdu.atype = MP3_2C_44100_128000

			case "AAC_2C_44100_192000":
				pdu.atype = AAC_2C_44100_192000

			case "AAC_2C_44100_128000":
				pdu.atype = AAC_2C_44100_128000

			case "MP3_1C_44100_48000":
				pdu.atype = MP3_1C_44100_48000

			case "AAC_2C_44100_24000":
				pdu.atype = AAC_2C_44100_24000

			default:
				log.Println("OOPS", ice_ainfo)
				pdu.atype = MP3_2C_44100_128000
			}

			headers := make([]string, 0)

			for _, k := range []string{
				"Icy-Genre", "Icy-Description", "Icy-Name", "Icy-Url",
				"Content-Type", "Icy-Private", "Icy-Pub"} {
				if v, ok := i.Headers[k]; ok {
					headers = append(headers, fmt.Sprintf("%s\r%s", k, v))
				}
			}

			pdu.headers = strings.Join(headers, "\n")
		}

		if is_meta {
			log.Println("META", string(buff))
			pdu.mtype = DAVECAST_ANNOUNCE
			dc <- pdu

			pdu.mtype = DAVECAST_METADATA
			pdu.metadata = string(buff)
			dc <- pdu

			pdu.mtype = DAVECAST_HEADERS
			dc <- pdu
		} else {
			parser(buff, func(b []byte) {
				//log.Println("DATA", len(b))
				pdu.mtype = DAVECAST_DATA
				pdu.data = b
				dc <- pdu
			})
		}
	})
}

func other_parser() func([]byte, func([]byte)) {
	var frame [65536]byte
	var last byte = 0x00
	offs := 0

	f := func(buff []byte, callback func([]byte)) {

		for n := 0; n < len(buff); n++ {
			frame[offs] = buff[n]

			if last == 0xff && buff[n]&0xf0 == 0xf0 {

				switch offs {

				case 0:
					frame[0] = last
					frame[1] = buff[n]
					offs = 2

				case 1:

				default:
					size := offs - 1
					var tmp = make([]byte, size)
					copy(tmp[:], frame[0:size])
					callback(tmp[:])
					frame[0] = last
					frame[1] = buff[n]
					offs = 2
					last = 0x00
				}

			} else {
				last = buff[n]
				offs++
			}
		}
	}

	return f
}



















