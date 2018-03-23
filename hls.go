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
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"time"

	"adts"
	"icecast"
)

type mountpoint struct {
	latest int
	chunks map[int][]byte
	bandwidth int
	content string
}

var mountpoints map[string]*mountpoint

func main() {
	mountpoints = make(map[string]*mountpoint)
	address := os.Args[1]
	server := os.Args[2]

	for n := 3; n < len(os.Args); n++ {
		var mp mountpoint
		go icyclient(server, os.Args[n], &mp)
		mountpoints[os.Args[n]] = &mp
	}

	hls_server(address)
}

func icyclient(server string, mountpoint string, mp *mountpoint) {
	source := fmt.Sprintf("http://%s/%s", server, mountpoint)

	defer func() {
		time.Sleep(3000 * time.Millisecond)
		go icyclient(server, mountpoint, mp)
	}()

	stream := make(chan []byte, 100)

	mp.latest = 0
	mp.chunks = nil

	go chunker(stream, mountpoint, mp)

	frames := adts.NIL()

	r := icecast.Open(source, func(buff []byte, meta bool, i icecast.Icecast) {
		if frames == nil {
			switch i.ContentType {
			case "audio/aac":
				frames = adts.ADTS()
			case "audio/aacp":
				frames = adts.ADTS()
			case "audio/mpeg":
				frames = adts.MPEG()
			default:
				panic(i.ContentType)
			}
			mp.bandwidth = i.BitRate * 1000
			mp.content = i.ContentType
		}

		if meta {
			//f := adts.AdtsMetadataFrame(buff)
			//stream <- f
			max := 50
			if len(buff) < max {
				max = len(buff)
			}
			log.Printf("%d: %s\n", i.MetadataInterval, string(buff[0:max]))
		} else {
			frames(buff, func(b []byte) { stream <- b })
		}
	})

	log.Println(source, "returned", r)

	close(stream)
}

func chunker(stream chan []byte, mountpoint string, mp *mountpoint) {
	start := time.Now().Unix() / 10
	current := start
	list := make([][]byte, 0)

	mp.chunks = make(map[int][]byte)

	for {
		if chunk, ok := <-stream; ok {

			if now := time.Now().Unix() / 10; now != current {

				mp.latest = int(current)
				mp.chunks[mp.latest] = join_frames(list)

				//log.Printf("saved %d bytes to key %d", len(chunk), mp.latest)
				write_chunk(mountpoint, mp.latest, mp.chunks[mp.latest])
				for len(mp.chunks) > 10 {
					var keys []int
					for k := range mp.chunks {
						keys = append(keys, k)
					}
					sort.Ints(keys)
					//log.Printf("deleting key %d", keys[0])
					delete(mp.chunks, keys[0])
				}

				//log.Println(mountpoint, current, len(list))
				list = make([][]byte, 0)
				current = now
			}

			list = append(list, chunk)
		} else {
			return
		}
	}
}

func hls_server(addr string) {

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("r %s\n", r.RequestURI)
		w.Header().Set("X-Address", addr)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Server", "Anarcast")

		if r.RequestURI == "/" {
			w.WriteHeader(http.StatusOK)
			return
		}

		re := regexp.MustCompile("^/([A-Za-z0-9.-_]+)/(|playlist.m3u8|chunklist.m3u8|\\d+)$")
		match := re.FindStringSubmatch(r.RequestURI)

		if match == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var mp *mountpoint

		if m, ok := mountpoints[match[1]]; !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		} else {
			mp = m
		}

		if mp.chunks == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		if _, ok := mp.chunks[mp.latest-3]; !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}

		switch match[2] {

		case "":
			fallthrough
		case "playlist.m3u8":
			bandwidth := fmt.Sprintf("BANDWIDTH=%d", mp.bandwidth)
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n")
			fmt.Fprintf(w, "#EXT-X-STREAM-INF:PROGRAM-ID=1,%s,CODECS=\"mp4a.40.2\"\n", bandwidth)
			//fmt.Fprintf(w, "#EXT-X-STREAM-INF:PROGRAM-ID=1,BANDWIDTH=128000,CODECS=\"mp4a.40.2\"\n")
			fmt.Fprintf(w, "chunklist.m3u8\n")

		case "chunklist.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "#EXTM3U\n")
			fmt.Fprintf(w, "#EXT-X-ALLOW-CACHE:NO\n")
			fmt.Fprintf(w, "#EXT-X-TARGETDURATION:10\n")
			fmt.Fprintf(w, "#EXT-X-MEDIA-SEQUENCE:%d\n", mp.latest)
			fmt.Fprintf(w, "#EXTINF:10,\n%d\n", mp.latest-2)
			fmt.Fprintf(w, "#EXTINF:10,\n%d\n", mp.latest-1)
			fmt.Fprintf(w, "#EXTINF:10,\n%d\n", mp.latest)

		default:
			//w.Header().Set("Content-Type", "media/x-acc")
			w.Header().Set("Content-Type", mp.content)
			if p, err := strconv.Atoi(match[2]); err == nil {
				if buff := mp.chunks[p]; buff != nil {
					w.Header().Set("Content-Length", fmt.Sprintf("%d", len(buff)))
					w.WriteHeader(http.StatusOK)
					fmt.Fprintf(w, "%s", buff)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		}
	})

	log.Fatal(http.ListenAndServe(addr, nil))
}

func write_chunk(mountpoint string, timestamp int, data []byte) {

	tempfile := fmt.Sprintf("%s/%010d", mountpoint, timestamp)

	os.Mkdir(mountpoint, 0755)

	if ioutil.WriteFile(tempfile, data, 0644) != nil {
		os.Remove(tempfile)
	}
	os.Rename(tempfile, tempfile+".adts")
}

func join_frames(list [][]byte) []byte {

	total := 0

	for n := 0; n < len(list); n++ {
		total += len(list[n])
	}

	buff := make([]byte, total)

	s := 0
	for n := 0; n < len(list); n++ {
		src := list[n]
		copy(buff[s:], src[:])
		s += len(list[n])
	}

	return buff
}
