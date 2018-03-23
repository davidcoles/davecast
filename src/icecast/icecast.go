package icecast

import (
	"io"
	"net/http"
	"log"
	"strconv"
	"strings"
)

type Icecast struct {
	endpoint string
	callback func([]byte, bool, Icecast)
	headers map[string][]string

	SampleRate int
    BitRate int
    Channels int
	
	ContentType string
	Headers map[string]string
	MetadataInterval uint
	Metadata []byte
	
	Genre string
	Description string
	Name string
	Url string
	Private bool
	Pub bool

}

type Connection []byte

func (i Icecast) Endpoint() string {
	return i.endpoint
}

func Open(endpoint string, callback func([]byte, bool, Icecast)) (int) {
	var i Icecast
	i.endpoint = endpoint
	i.callback = callback

	client := &http.Client{
		//CheckRedirect: redirectPolicyFunc,
	}
	
	req, err := http.NewRequest("GET", endpoint, nil)
	req.Header.Add("Icy-MetaData", "1")
	resp, err := client.Do(req)

	if resp.StatusCode != 200 {
		return resp.StatusCode
	}

	if err != nil {
		log.Println(endpoint, "doh", err)
		return -1
	}
	
	defer resp.Body.Close()

	metaint := 0

	if ice_metadata, ok := resp.Header["Icy-Metaint"]; ok {
		
		if p, err := strconv.Atoi(ice_metadata[0]); err != nil {
			log.Println(endpoint, "Icy-Metaint must be an integer")
			return -1
		} else {
			metaint = p
			i.MetadataInterval = uint(p)
		}
	}
	
	i.headers = resp.Header

	if header, ok := resp.Header["Content-Type"]; ok {
		i.ContentType = header[0]
	}
	
	if ice_info, ok := resp.Header["Ice-Audio-Info"]; ok {
		info := strings.Split(ice_info[0], ";")

		i.SampleRate = 0
		i.BitRate = 0
		i.Channels = 0
		
		for _, v := range info {
			param := strings.Split(v, "=")
			p, _ := strconv.Atoi(param[1])
			
			switch param[0] {
			case "ice-samplerate":
				i.SampleRate = p
				
			case "ice-bitrate":
				i.BitRate = p
				
			case "ice-channels":
				i.Channels = p
			}
		}
	}

	if h, ok := resp.Header["Icy-Genre"]; ok {
		i.Genre = h[0];
	}

	if h, ok := resp.Header["Icy-Description"]; ok {
		i.Description = h[0];
	}

	if h, ok := resp.Header["Icy-Name"]; ok {
		i.Name = h[0];
	}

	if h, ok := resp.Header["Icy-Url"]; ok {
		i.Url = h[0];
	}

	if h, ok := resp.Header["Icy-Private"]; ok {
		i.Private = false
		if h[0] == "1" {
			i.Private = true
		}
	}

	if h, ok := resp.Header["Icy-Pub"]; ok {
		i.Pub = true
		if h[0] == "0" {
			i.Pub = false
		}
	}

	if h, ok := resp.Header["Icy-Br"]; ok {
		if i.BitRate == 0 {
			p, _ := strconv.Atoi(h[0])
			i.BitRate = p
		}
	}

	chunk := metaint

	if chunk == 0 {
		chunk = 8192
	}

	for {
		// read chunk bytes
		buff := make([]byte, chunk)
		if nread, _ := io.ReadFull(resp.Body, buff); nread != chunk {
			log.Println(endpoint, "short read", nread, chunk)
			return -1
		}
		
		callback(buff, false, i)
		
		if metaint != 0 {

			// read 1 byte
			size := make([]byte, 1)
			if nread, _ := io.ReadFull(resp.Body, size); nread != 1 {
				log.Println(endpoint, "short read", nread, metaint)
				return -1
			}

			// read #bytes from prev step
			msiz := int(size[0]) * 16
			meta := make([]byte, msiz)
			if nread, _ := io.ReadFull(resp.Body, meta); nread != msiz {
				log.Println(endpoint, "short read", nread, msiz)
				return -1
			}
			i.Metadata = meta
			callback(meta, true, i)
		} else {
			callback([]byte("hello world"), true, i)
		}
	}
	return resp.StatusCode
}
