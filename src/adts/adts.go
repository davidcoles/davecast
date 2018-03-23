package adts

import (
//	"log"
)

type Frame []byte

func NewFrame(raw []byte) Frame {
	return Frame(raw)
}
func (f Frame) Raw () []byte {
	return f
}
func (f Frame) Syncword () int {
	return (int(f[0]) << 4) + (int(f[1]) >> 4)
}
func (f Frame) FrameLength () int {
	l := ( int(f[3]&3) << 11 ) + (int(f[4]) << 3) + (int(f[5]&224) >> 5)
	if l != len(f) {
		panic("l, f")
	}
	return len(f)
}
func (f Frame) MpegVersion () int {
	return int(f[1]&8) >> 3
}
func (f Frame) Layer () int {
	return int(f[1]&6) >> 1
}
func (f Frame) ProtectionAbsent () int {
	return int(f[1]&1)
}
func (f Frame) Profile () int {
	return int(f[2]&192) >> 6
}
func (f Frame) SamplingFrequencyIndex () int {
	return int(f[2]&60) >> 2
}
func (f Frame) Private () int {
	return int(f[2]&2) >> 1
}
func (f Frame) ChannelConfiguration () int {
	return (int(f[2]&1) << 2) + (int(f[3]&192) >>6)
}
func (f Frame) Originality () int {
	return int(f[3]&32) >> 5
}
func (f Frame) Home () int {
	return int(f[3]&16) >> 4
}
func (f Frame) CopyrightedIdBit () int {
	return int(f[3]&8) >> 3
}
func (f Frame) CopyrightedIdStart () int {
	return int(f[3]&4) >> 2
}
func (f Frame) BufferFullness () int {
	return (int(f[5]&31) << 6) + (int(f[6]&252) >> 2)
}
func (f Frame) NumberAACFrames () int {
	return int(f[6]&3)
}
func (f Frame) IsMetadata () bool {
	// signature: 0xff, 0xf1, 0x3c
	if f[0] == 0xff && f[1] == 0xf1 && f[2] == 0x3c {
		return true
	}
	return false
}
func (f Frame) Metadata () string {
	// signature: 0xff, 0xf1, 0x3c
	if f.IsMetadata()  {
		return string(f[7:])
	}
	return ""
}

func (f Frame) HeaderLength () int {
	// signature: 0xff, 0xf1, 0x3c
	if f.ProtectionAbsent() == 0  {
		return 9
	}
	return 7
}

func (f Frame) AACFrame () []byte {
	// signature: 0xff, 0xf1, 0x3c
	start := f.HeaderLength()
	//end := start + f.FrameLength()
	return f[start:]
}





func ADTS () func([]byte, func([]byte)) {
	var raw [65536]byte
	//var frame Frame

	pos := 0
	frameLength := 0
	
	f := func(buff []byte, fx func([]byte)) {
		
		for n := 0; n < len(buff); n++ {
			b := buff[n]
			
			if pos > 16384 {
				panic("too big")
			}
			
			switch pos {
			case 0: // AAAAAAAA
				if b != 0xff {
					pos = 0
					continue
				}
			case 1: // AAAABCCD
				if b & 0xf0 != 0xf0 {
					pos = 0
					continue
				}
			case 2: // EEFFFFGH
			case 3: // HHIJKLMM
				frameLength = int(b&3) << 11
			case 4: // MMMMMMMM
				frameLength += int(b) << 3
			case 5: // MMMOOOOO
				frameLength += int(b&224) >> 5
			case 6: // OOOOOOPP
			case 7: //(QQQQQQQQ
			case 8: // QQQQQQQQ) 
			}

			raw[pos] = b
			pos++
			
			if pos > 6 && pos == frameLength {
				d := make([]byte, frameLength)
				copy(d[:], raw[0:pos])
				//frame = d
				fx(d)
				pos = 0
			}
		}
	}

	return f;
}


func AdtsMetadataFrame (metadata []byte) []byte {
	// signature: 0xff, 0xf9, 0x3c

    m := 7 + len(metadata)
	o := 100

	adts := make([]byte, m)

	adts[0] = 0xff
	adts[1] = 0xf9
	adts[2] = 0x3c
	adts[3] = byte((m >> 11) & 0xff)
	adts[4] = byte((m >> 3) & 0xff)
	adts[5] = byte((m << 5) & 0xe0) | byte((o >> 6) & 0x1f)
	adts[6] = byte((o << 2) & 0xfc)

	copy(adts[7:], metadata[:])
	return adts[:]
}






func RAW() func([]byte, func([]byte)) {
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




func NIL () func([]byte, func([]byte)) {
	return nil
}


// http://mpgedit.org/mpgedit/mpeg_format/mpeghdr.htm
func MPEG () func([]byte, func([]byte)) {
	var raw [65536]byte

	pos := 0
	frameLength := 0
	
	version_id := 0
	layer_desc := 0
	bitrate_index := 0
	padding := 0
	sample_rate_frequency_index := 0

	f := func(buff []byte, fx func([]byte)) {
		
		for n := 0; n < len(buff); n++ {
			b := buff[n]
			
			if pos > 16384 {
				panic("too big")
			}

			// AAAAAAAA AAABBCCD EEEEFFGH IIJJKLMM 
			// A 11 (31-21) Frame sync (all bits set)
			// B 2  (20,19) MPEG Audio version ID
			// C 2  (18,17) Layer description
			// D 1  (16)    Protection bit
			// E 4  (15,12) Bitrate index
			// F 2  (11,10) Sampling rate frequency index (values are in Hz) 
			// G 1  (9)     Padding bit
			// H 1  (8)     Private bit. 
			// I 2  (7,6)   Channel Mode
			// J 2  (5,4)   Mode extension (Only if Joint stereo) 
			// K 1  (3)     Copyright
			// L 1  (2)     Original
			// M 2  (1,0)   Emphasis
			
			switch pos {
			case 0: // AAAAAAAA
				if b != 0xff {
					panic("0")
					pos = 0
					continue
				}
			case 1: // AAABBCCD
				if b & 0xe0 != 0xe0 {
					panic("1")
					pos = 0
					continue
				}
				version_id = int((b & 0x18)>>3)
				layer_desc = int((b & 0x6)>>1)
				//protection = int(b & 0x01)
			case 2: // EEEEFFGH
				bitrate_index = int((b & 0xf0) >> 4)
				sample_rate_frequency_index = int((b & 0xc) >> 2)
				padding = int((b & 0x2) >> 1)
				//private = int(b & 0x1) 
			case 3: // IIJJKLMM
				//channel_mode = int((b & 0xc0) >> 6)
				//mode_ext = int((b & 0x30) >> 4)
				//copyright = int((b & 0x08) >> 3)
				//original = int((b & 0x04) >> 2)
				//empahasis = int((b & 0x03))

				br := mpegBitrate(version_id, layer_desc, bitrate_index)
				sr := mpegSampleRate(version_id, sample_rate_frequency_index)
				
				if layer_desc == 3 { // L1: frame_size=384 slot_length=4
					frameLength = (12 * br / sr + padding) * 4 
				} else {  // L2+3: frame_size=1152 slot_length=1 
					frameLength = 144 * br / sr + padding
				}
				
				//log.Printf(">>> %v %v %v\n", br, sr, frameLength)
				if br == -1 || sr == -1 {
					panic("br/sr")
				}
				
			}

			raw[pos] = b
			pos++
			
			if pos > 3 && pos == frameLength {
				d := make([]byte, frameLength)
				copy(d[:], raw[0:pos])
				fx(d)
				pos = 0
			}
		}
	}
	
	return f;
}

func mpegSampleRate(version_id int, sample_rate_frequency_index int)(int) {
	// 0 00 - MPEG Version 2.5
	// 1 01 - reserved
	// 2 10 - MPEG Version 2 (ISO/IEC 13818-3)
	// 3 11 - MPEG Version 1 (ISO/IEC 11172-3) 
	
	if version_id == 3 {
		switch(sample_rate_frequency_index) {
		case 0: return 44100
		case 1: return 48000
		case 2: return 32000
		case 3: return -1
		}
	}

	if version_id == 2 {
		switch(sample_rate_frequency_index) {
		case 0: return 22050
		case 1: return 24000
		case 2: return 16000
		case 3: return -1
		}
	}

	if version_id == 0 {
		switch(sample_rate_frequency_index) {
		case 0: return 11025
		case 1: return 12000
		case 2: return 8000
		case 3: return -1
		}
	}

	panic("sample_rate")
	return -1
}

func mpegBitrate(version_id int, layer_desc int, bitrate_index int) (int) {
	// version_id
	// 0 00 - MPEG Version 2.5
	// 1 01 - reserved
	// 2 10 - MPEG Version 2 (ISO/IEC 13818-3)
	// 3 11 - MPEG Version 1 (ISO/IEC 11172-3) 

	// layer_desc
	// 0 00 - reserved
	// 1 01 - Layer III
	// 2 10 - Layer II
	// 3 11 - Layer I

	// V1/L3
	if version_id == 3 && layer_desc == 1 {
		switch(bitrate_index) {
		case  0: return      0
		case  1: return  32000
		case  2: return  40000
		case  3: return  48000
		case  4: return  56000
		case  5: return  64000
		case  6: return  80000
		case  7: return  96000
		case  8: return 112000
		case  9: return 128000
		case 10: return 160000
		case 11: return 192000
		case 12: return 224000
		case 13: return 256000
		case 14: return 320000
		case 15: return -1
		}
	}

    // V1/L2
	if version_id == 3 && layer_desc == 2 {
		switch(bitrate_index) {
		case  0: return      0
		case  1: return  32000
		case  2: return  48000
		case  3: return  56000
		case  4: return  64000
		case  5: return  80000
		case  6: return  96000
		case  7: return 112000
		case  8: return 128000
		case  9: return 160000
		case 10: return 192000
		case 11: return 224000
		case 12: return 256000
		case 13: return 320000
		case 14: return 384000
		case 15: return -1
		}
	}

    // V1/L1
	if version_id == 3 && layer_desc == 3 {
		switch(bitrate_index) {
		case  0: return      0
		case  1: return  32000
		case  2: return  64000
		case  3: return  96000
		case  4: return 128000
		case  5: return 160000
		case  6: return 192000
		case  7: return 224000
		case  8: return 256000
		case  9: return 288000
		case 10: return 320000
		case 11: return 352000
		case 12: return 384000
		case 13: return 416000
		case 14: return 448000
		case 15: return -1
		}
	}


    // V2/L1
	if version_id == 2 && layer_desc == 3 {
		switch(bitrate_index) {
		case  0: return      0
		case  1: return  32000
		case  2: return  48000
		case  3: return  56000
		case  4: return  64000
		case  5: return  80000
		case  6: return  96000
		case  7: return 112000
		case  8: return 128000
		case  9: return 144000
		case 10: return 160000
		case 11: return 176000
		case 12: return 192000
		case 13: return 224000
		case 14: return 256000
		case 15: return -1
		}
	}
	
    // V2/L2&L3
	if version_id == 2 && (layer_desc == 1 || layer_desc == 2 ){
		switch(bitrate_index) {
		case  0: return      0
		case  1: return   8000
		case  2: return  16000
		case  3: return  24000
		case  4: return  32000
		case  5: return  40000
		case  6: return  48000
		case  7: return  56000
		case  8: return  64000
		case  9: return  80000
		case 10: return  96000
		case 11: return 112000
		case 12: return 128000
		case 13: return 144000
		case 14: return 160000
		case 15: return -1
		}
	}
	
	

	
	panic("bitrate")
	return -1
}



































