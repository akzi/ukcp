//=====================================================================
//
// KCP - A Better ARQ Protocol Implementation
// skywind3000 (at) gmail.com, 2010-2011
//
// Features:
// + Average RTT reduce 30% - 40% vs traditional ARQ like tcp.
// + Maximum RTT reduce three times vs tcp.
// + Lightweight, distributed as a single source file.
//
//=====================================================================
package ikcp

import (
	"container/list"
	"encoding/binary"
)

const (
	RTO_NDL     uint32 = 30  // no delay min rto
	RTO_MIN     uint32 = 100 // normal min rto
	RTO_DEF     uint32 = 200
	RTO_MAX     uint32 = 60000
	CMD_PUSH    uint32 = 81 // cmd: push data
	CMD_ACK     uint32 = 82 // cmd: ack
	CMD_WASK    uint32 = 83 // cmd: window probe (ask)
	CMD_WINS    uint32 = 84 // cmd: window size (tell)
	ASK_SEND    uint32 = 1  // need to send CMD_WASK
	ASK_TELL    uint32 = 2  // need to send CMD_WINS
	WND_SND     uint32 = 32
	WND_RCV     uint32 = 32
	MTU_DEF     uint32 = 1400
	ACK_FAST    uint32 = 3
	INTERVAL    uint32 = 100
	OVERHEAD    uint32 = 24
	DEADLINK    uint32 = 10
	THRESH_INIT uint32 = 2
	THRESH_MIN  uint32 = 2
	PROBE_INIT  uint32 = 7000   // 7 secs to probe window size
	PROBE_LIMIT uint32 = 120000 // up to 120 secs to probe window
)

// encode 8 bits unsigned int
func encode8u(p []byte, c byte) []byte {
	p[0] = c
	return p[1:]
}

// decode 8 bits unsigned int
func decode8u(p []byte, c *byte) []byte {
	*c = p[0]
	return p[1:]
}

// encode 16 bits unsigned int (lsb)
func encode16u(p []byte, w uint16) []byte {
	binary.LittleEndian.PutUint16(p, w)
	return p[2:]
}

// decode 16 bits unsigned int (lsb)
func decode16u(p []byte, w *uint16) []byte {
	*w = binary.LittleEndian.Uint16(p)
	return p[2:]
}

// encode 32 bits unsigned int (lsb)
func encode32u(p []byte, l uint32) []byte {
	binary.LittleEndian.PutUint32(p, l)
	return p[4:]
}

/* decode 32 bits unsigned int (lsb) */
func decode32u(p []byte, l *uint32) []byte {
	*l = binary.LittleEndian.Uint32(p)
	return p[4:]
}

func _imin_(a, b uint32) uint32 {
	if a <= b {
		return a
	} else {
		return b
	}
}

func _imax_(a, b uint32) uint32 {
	if a >= b {
		return a
	} else {
		return b
	}
}

func _ibound_(lower, middle, upper uint32) uint32 {
	return _imin_(_imax_(lower, middle), upper)
}

func _itimediff(later, earlier uint32) int32 {
	return ((int32)(later - earlier))
}

// manage segment
type IKCPSEG struct {
	conv     uint32
	cmd      uint32
	frg      uint32
	wnd      uint32
	ts       uint32
	sn       uint32
	una      uint32
	_len     uint32
	resendts uint32
	rto      uint32
	fastack  uint32
	xmit     uint32
	data     []byte //1 size
}

/*
static void* (*malloc_hook)(size_t) = nil
static void (*free_hook)(void *) = nil

// internal malloc
static void* malloc(size_t size) {
        if (malloc_hook)
        return malloc_hook(size)
        return malloc(size)
}

// internal free
static void free(void *ptr) {
        if (free_hook) {
                free_hook(ptr)
        }	else {
                free(ptr)
        }
}
// redefine allocator
void allocator(void* (*new_malloc)(size_t), void (*new_free)(void*))
{
        malloc_hook = new_malloc
        free_hook = new_free
}
*/

// create a new kcpcb
func Create(conv uint32, user interface{}) *Ikcpcb {
	kcp := &Ikcpcb{
		conv:     conv,
		user:     user,
		acklist:  nil,
		sndQueue: list.New(),
		rcvQueue: list.New(),
		sndBuf:   list.New(),
		rcvBuf:   list.New(),
		sndWnd:   WND_SND,
		rcvWnd:   WND_RCV,
		rmtWnd:   WND_RCV,
		mtu:      MTU_DEF,
		mss:      MTU_DEF - OVERHEAD,
		rxRto:    RTO_DEF,
		rxMinrto: RTO_MIN,
		interval: INTERVAL,
		ts_flush: INTERVAL,
		ssthresh: THRESH_INIT,
		deadLink: DEADLINK,
		buffer:   make([]byte, (MTU_DEF+OVERHEAD)*3),
	}

	return kcp
}

type Ikcpcb struct {
	conv, mtu, mss, state               uint32
	sndUna, sndNxt, rcvNxt              uint32
	tsRecent, tsLastack, ssthresh       uint32
	rxRttval, rxSrtt, rxRto, rxMinrto   uint32
	sndWnd, rcvWnd, rmtWnd, cwnd, probe uint32
	current, interval, ts_flush, xmit   uint32
	nrcvBuf, nsndBuf                    uint32
	nrcvQue, nsndQue                    uint32
	nodelay, updated                    uint32
	tsProbe, probeWait                  uint32
	deadLink, incr                      uint32
	sndQueue, rcvQueue, sndBuf, rcvBuf  *list.List
	acklist                             []uint32
	ackcount                            uint32
	ackblock                            uint32
	user                                interface{}
	buffer                              []byte
	fastresend                          int32
	nocwnd                              int32
	logmask                             int32
	writelog                            func(log []byte, kcp *Ikcpcb, user []byte)

	Output func(buf []byte, _len int32, kcp *Ikcpcb, user interface{}) int32
}

// allocate a new kcp segment
func (kcp *Ikcpcb) segmentNew(size int32) *IKCPSEG {
	newInfo := &IKCPSEG{}
	newInfo.data = make([]byte, size)
	return newInfo
}

// output segment
func (kcp *Ikcpcb) output(data []byte, size int32) int32 {
	if size == 0 {
		return 0
	}
	return kcp.Output(data, size, kcp, kcp.user)
}

// release a new kcpcb
func (kcp *Ikcpcb) Release() {
	kcp.nrcvBuf = 0
	kcp.nsndBuf = 0
	kcp.nrcvQue = 0
	kcp.nsndQue = 0
	kcp.ackcount = 0
	kcp.buffer = nil
	kcp.acklist = nil
}

// recv data
func (kcp *Ikcpcb) Recv(buffer []byte, _len int32) int32 {
	ispeek := 1
	if _len >= 0 {
		ispeek = 0
	}
	var peeksize int32
	_recover := 0
	var seg *IKCPSEG

	if kcp.rcvQueue.Len() == 0 {
		return -1
	}

	if _len < 0 {
		_len = -_len
	}

	peeksize = kcp.Peeksize()

	if peeksize < 0 {
		return -2
	}

	if peeksize > _len {
		return -3
	}

	if kcp.nrcvQue >= kcp.rcvWnd {
		_recover = 1
	}

	//if kcp.user[0] == 0 {
	//fmt.Println("have!!!!")
	//}
	// merge fragment
	_len = 0
	for p := kcp.rcvQueue.Front(); p != nil; {
		var fragment int32
		seg = p.Value.(*IKCPSEG)

		if len(buffer) > 0 {
			copy(buffer, seg.data[:seg._len])
			buffer = buffer[seg._len:]
		}

		_len += int32(seg._len)
		fragment = int32(seg.frg)

		//if canlog(kcp, LOG_RECV) != 0 {
		//	log(kcp, LOG_RECV, "recv sn=", seg.sn, seg._len, kcp.user)
		//}

		if ispeek == 0 {
			q := p.Next()
			kcp.rcvQueue.Remove(p)
			p = q
			kcp.nrcvQue--
			//if kcp.user[0] == 0 {
			//fmt.Println("remove from recvqueue", kcp.rcv_queue.Len(), kcp.user, "rcv q:", kcp.nrcv_que)
			//}
		} else {
			p = p.Next()
		}

		if fragment == 0 {
			break
		}
	}
	// move available data from rcv_buf . rcv_queue
	for p := kcp.rcvBuf.Front(); p != nil; {
		seg := p.Value.(*IKCPSEG)
		if seg.sn == kcp.rcvNxt && kcp.nrcvQue < kcp.rcvWnd {
			q := p.Next()
			kcp.rcvBuf.Remove(p)
			p = q
			kcp.nrcvBuf--
			kcp.rcvQueue.PushBack(seg)
			kcp.nrcvQue++
			//if kcp.user[0] == 0 {
			//fmt.Println("insert from recvqueue", kcp.rcv_queue.Len(), kcp.user, "rcv q:", kcp.nrcv_que)
			//}
			kcp.rcvNxt++
		} else {
			break
		}
	}

	// fast _recover
	if kcp.nrcvQue < kcp.rcvWnd && _recover != 0 {
		// ready to send back CMD_WINS in Ikcp_flush
		// tell remote my window size
		kcp.probe |= ASK_TELL
	}

	return _len
}

// send data
func (kcp *Ikcpcb) Peeksize() int32 {
	length := 0

	if kcp.rcvQueue.Len() == 0 {
		return -1
	}

	seg := kcp.rcvQueue.Front().Value.(*IKCPSEG)
	if seg.frg == 0 {
		return int32(seg._len)
	}

	if kcp.nrcvQue < seg.frg+1 {
		return -1
	}

	for p := kcp.rcvQueue.Front(); p != nil; p = p.Next() {
		seg = p.Value.(*IKCPSEG)
		length += int(seg._len)
		if seg.frg == 0 {
			break
		}
	}

	return int32(length)
}

// send data
func (kcp *Ikcpcb) Send(buffer []byte, _len int) int {
	var seg *IKCPSEG
	var count, i int32

	if _len < 0 {
		return -1
	}

	if _len <= int(kcp.mss) {
		count = 1
	} else {
		count = (int32(_len) + int32(kcp.mss) - 1) / int32(kcp.mss)
	}

	if count > 255 {
		return -2
	}

	if count == 0 {
		count = 1
	}

	// fragment
	for i = 0; i < count; i++ {
		size := int32(kcp.mss)
		if _len <= int(kcp.mss) {
			size = int32(_len)
		}
		seg = kcp.segmentNew(size)
		if seg == nil {
			return -2
		}
		if buffer != nil && _len > 0 {
			copy(seg.data, buffer[:size])
		}
		seg._len = uint32(size)
		seg.frg = uint32(count - i - 1)
		kcp.sndQueue.PushBack(seg)
		//if kcp.user[0] == 0 {
		//fmt.Println(kcp.user, "send", kcp.snd_queue.Len())
		//}
		kcp.nsndQue++
		if buffer != nil {
			buffer = buffer[size:]
		}
		_len -= int(size)
	}

	return 0
}

// parse ack
func (kcp *Ikcpcb) UpdateAck(rtt int32) {
	rto := 0
	if kcp.rxSrtt == 0 {
		kcp.rxSrtt = uint32(rtt)
		kcp.rxRttval = uint32(rtt) / 2
	} else {
		delta := uint32(rtt) - kcp.rxSrtt
		if delta < 0 {
			delta = -delta
		}
		kcp.rxRttval = (3*kcp.rxRttval + delta) / 4
		kcp.rxSrtt = (7*kcp.rxSrtt + uint32(rtt)) / 8
		if kcp.rxSrtt < 1 {
			kcp.rxSrtt = 1
		}
	}
	rto = int(kcp.rxSrtt + _imax_(1, 4*kcp.rxRttval))
	kcp.rxRto = _ibound_(kcp.rxMinrto, uint32(rto), RTO_MAX)
}

func (kcp *Ikcpcb) shrinkBuf() {
	if kcp.sndBuf.Len() > 0 {
		p := kcp.sndBuf.Front()
		seg := p.Value.(*IKCPSEG)
		kcp.sndUna = seg.sn
		//if kcp.user[0] == 0 {
		//println("set snd_una:", seg.sn)
		//}
	} else {
		kcp.sndUna = kcp.sndNxt
		//if kcp.user[0] == 0 {
		//println("set2 snd_una:", kcp.snd_nxt)
		//}
	}
}

func (kcp *Ikcpcb) parseAck(sn uint32) {
	if _itimediff(sn, kcp.sndUna) < 0 || _itimediff(sn, kcp.sndNxt) >= 0 {
		//fmt.Printf("wi %d,%d  %d,%d\n", sn, kcp.snd_una, sn, kcp.snd_nxt)
		return
	}

	for p := kcp.sndBuf.Front(); p != nil; p = p.Next() {
		seg := p.Value.(*IKCPSEG)
		if sn == seg.sn {
			//println("!!!!!!!")
			kcp.sndBuf.Remove(p)
			kcp.nsndBuf--
			break
		} else {
			seg.fastack++
		}
	}
}

func (kcp *Ikcpcb) parseUna(una uint32) {
	for p := kcp.sndBuf.Front(); p != nil; {
		seg := p.Value.(*IKCPSEG)
		if _itimediff(una, seg.sn) > 0 {
			q := p.Next()
			kcp.sndBuf.Remove(p)
			p = q
			kcp.nsndBuf--
		} else {
			break
		}
	}
}

// ack append
func (kcp *Ikcpcb) ackPush(sn, ts uint32) {
	newsize := kcp.ackcount + 1

	if newsize > kcp.ackblock {
		var acklist []uint32
		var newblock int32

		for newblock = 8; uint32(newblock) < newsize; newblock <<= 1 {
		}
		acklist = make([]uint32, newblock*2)
		if kcp.acklist != nil {
			for x := 0; uint32(x) < kcp.ackcount; x++ {
				acklist[x*2+0] = kcp.acklist[x*2+0]
				acklist[x*2+1] = kcp.acklist[x*2+1]
			}
		}
		kcp.acklist = acklist
		kcp.ackblock = uint32(newblock)
	}

	ptr := kcp.acklist[kcp.ackcount*2:]
	ptr[0] = sn
	ptr[1] = ts
	kcp.ackcount++
}

func (kcp *Ikcpcb) ackGet(p int32, sn, ts *uint32) {
	if sn != nil {
		*sn = kcp.acklist[p*2+0]
	}
	if ts != nil {
		*ts = kcp.acklist[p*2+1]
	}
}

// parse data
func (kcp *Ikcpcb) parseData(newseg *IKCPSEG) {
	var p *list.Element
	sn := newseg.sn
	repeat := 0
	if _itimediff(sn, kcp.rcvNxt+kcp.rcvWnd) >= 0 ||
		_itimediff(sn, kcp.rcvNxt) < 0 {
		return
	}

	for p = kcp.rcvBuf.Back(); p != nil; p = p.Prev() {
		seg := p.Value.(*IKCPSEG)
		if seg.sn == sn {
			repeat = 1
			break
		}
		if _itimediff(sn, seg.sn) > 0 {
			break
		}
	}

	if repeat == 0 {
		if p == nil {
			kcp.rcvBuf.PushFront(newseg)
		} else {
			kcp.rcvBuf.InsertAfter(newseg, p)
		}
		kcp.nrcvBuf++
	} else {
	}
	for p = kcp.rcvBuf.Front(); p != nil; {
		seg := p.Value.(*IKCPSEG)
		if seg.sn == kcp.rcvNxt && kcp.nrcvQue < kcp.rcvWnd {
			q := p.Next()
			kcp.rcvBuf.Remove(p)
			p = q
			kcp.nrcvBuf--
			kcp.rcvQueue.PushBack(seg)
			//if kcp.user[0] == 0 {
			//fmt.Println("insert from recvqueue2", kcp.rcv_queue.Len(), kcp.user)
			//}
			kcp.nrcvQue++
			kcp.rcvNxt++
		} else {
			break
		}
	}
	//println("inputok!!!", kcp.nrcv_buf, kcp.nrcv_que)
}

// input data
func (kcp *Ikcpcb) Input(data []byte, size int) int {
	una := kcp.sndUna
	//if canlog(kcp, LOG_INPUT) != 0 {
	//	log(kcp, LOG_INPUT, "[RI] %d bytes", size)
	//}

	if data == nil || size < 24 {
		return 0
	}

	for {
		var ts, sn, _len, una, conv uint32
		var wnd uint16
		var cmd, frg uint8
		var seg *IKCPSEG

		if size < int(OVERHEAD) {
			break
		}

		data = decode32u(data, &conv)
		if conv != kcp.conv {
			return -1
		}

		data = decode8u(data, &cmd)
		data = decode8u(data, &frg)
		data = decode16u(data, &wnd)
		data = decode32u(data, &ts)
		data = decode32u(data, &sn)
		data = decode32u(data, &una)
		data = decode32u(data, &_len)

		size -= int(OVERHEAD)

		if uint32(size) < uint32(_len) {
			return -2
		}

		if cmd != uint8(CMD_PUSH) && cmd != uint8(CMD_ACK) &&
			cmd != uint8(CMD_WASK) && cmd != uint8(CMD_WINS) {
			return -3
		}

		kcp.rmtWnd = uint32(wnd)
		kcp.parseUna(una)
		kcp.shrinkBuf()

		if cmd == uint8(CMD_ACK) {
			if _itimediff(kcp.current, ts) >= 0 {
				kcp.UpdateAck(_itimediff(kcp.current, ts))
			}
			kcp.parseAck(sn)
			kcp.shrinkBuf()
			//if canlog(kcp, LOG_IN_ACK) != 0 {
			//	log(kcp, LOG_IN_DATA,
			//		"input ack: sn=%lu rtt=%ld rto=%ld", sn,
			//		uint32(_itimediff(kcp.current, ts)),
			//		uint32(kcp.rxRto))
			//}
		} else if cmd == uint8(CMD_PUSH) {
			//if canlog(kcp, LOG_IN_DATA) != 0 {
			//	log(kcp, LOG_IN_DATA,
			//		"input psh: sn=%lu ts=%lu", sn, ts)
			//}
			if _itimediff(sn, kcp.rcvNxt+kcp.rcvWnd) < 0 {
				kcp.ackPush(sn, ts)
				if _itimediff(sn, kcp.rcvNxt) >= 0 {
					seg = kcp.segmentNew(int32(_len))
					seg.conv = conv
					seg.cmd = uint32(cmd)
					seg.frg = uint32(frg)
					seg.wnd = uint32(wnd)
					seg.ts = ts
					seg.sn = sn
					seg.una = una
					seg._len = _len

					if _len > 0 {
						copy(seg.data, data[:_len])
					}

					kcp.parseData(seg)
				}
			}
		} else if cmd == uint8(CMD_WASK) {
			// ready to send back CMD_WINS in Ikcp_flush
			// tell remote my window size
			kcp.probe |= ASK_TELL
			//if canlog(kcp, LOG_IN_PROBE) != 0 {
			//	log(kcp, LOG_IN_PROBE, "input probe")
			//}
		} else if cmd == uint8(CMD_WINS) {
			// do nothing
			//if canlog(kcp, LOG_IN_WIN) != 0 {
			//	log(kcp, LOG_IN_WIN,
			//		"input wins: %lu", uint32(wnd))
			//}
		} else {
			return -3
		}

		data = data[_len:]
		size -= int(_len)
	}

	if _itimediff(kcp.sndUna, una) > 0 {
		if kcp.cwnd < kcp.rmtWnd {
			mss := kcp.mss
			if kcp.cwnd < kcp.ssthresh {
				kcp.cwnd++
				kcp.incr += mss
			} else {
				if kcp.incr < mss {
					kcp.incr = mss
				}
				kcp.incr += (mss*mss)/kcp.incr + (mss / 16)
				if (kcp.cwnd+1)*mss >= kcp.incr {
					kcp.cwnd++
				}
			}
			if kcp.cwnd > kcp.rmtWnd {
				kcp.cwnd = kcp.rmtWnd
				kcp.incr = kcp.rmtWnd * mss
			}
		}
	}

	return 0
}

// ikcp_encode_seg
func encodeSeg(ptr []byte, seg *IKCPSEG) []byte {
	ptr = encode32u(ptr, seg.conv)
	ptr = encode8u(ptr, uint8(seg.cmd))
	ptr = encode8u(ptr, uint8(seg.frg))
	ptr = encode16u(ptr, uint16(seg.wnd))
	ptr = encode32u(ptr, seg.ts)
	ptr = encode32u(ptr, seg.sn)
	ptr = encode32u(ptr, seg.una)
	ptr = encode32u(ptr, seg._len)
	return ptr
}

func (kcp *Ikcpcb) wndUnused() int32 {
	if kcp.nrcvQue < kcp.rcvWnd {
		return int32(kcp.rcvWnd - kcp.nrcvQue)
	}
	return 0
}

// Ikcp_flush
func (kcp *Ikcpcb) Flush() {
	current := kcp.current
	buffer := kcp.buffer
	ptr := buffer
	var count, size, i int32
	var resent, cwnd uint32
	var rtomin uint32
	change := 0
	lost := 0
	var seg IKCPSEG

	// 'Ikcp_update' haven't been called.
	if kcp.updated == 0 {
		return
	}

	seg.conv = kcp.conv
	seg.cmd = CMD_ACK
	seg.frg = 0
	seg.wnd = uint32(kcp.wndUnused())
	seg.una = kcp.rcvNxt
	seg._len = 0
	seg.sn = 0
	seg.ts = 0

	// flush acknowledges
	size = 0
	count = int32(kcp.ackcount)
	for i = 0; i < count; i++ {
		//size = int32(ptr - buffer)
		if size > int32(kcp.mtu) {
			kcp.output(buffer, size)
			ptr = buffer
			size = 0
		}
		kcp.ackGet(i, &seg.sn, &seg.ts)
		ptr = encodeSeg(ptr, &seg)
		size += 24
	}

	kcp.ackcount = 0

	// probe window size (if remote window size equals zero)
	if kcp.rmtWnd == 0 {
		if kcp.probeWait == 0 {
			kcp.probeWait = PROBE_INIT
			kcp.tsProbe = kcp.current + kcp.probeWait
		} else {
			if _itimediff(kcp.current, kcp.tsProbe) >= 0 {
				if kcp.probeWait < PROBE_INIT {
					kcp.probeWait = PROBE_INIT
				}
				kcp.probeWait += kcp.probeWait / 2
				if kcp.probeWait > PROBE_LIMIT {
					kcp.probeWait = PROBE_LIMIT
				}
				kcp.tsProbe = kcp.current + kcp.probeWait
				kcp.probe |= ASK_SEND
			}
		}
	} else {
		kcp.tsProbe = 0
		kcp.probeWait = 0
	}

	// flush window probing commands
	if (kcp.probe & ASK_SEND) != 0 {
		seg.cmd = CMD_WASK
		if size > int32(kcp.mtu) {
			kcp.output(buffer, size)
			ptr = buffer
			size = 0
		}
		ptr = encodeSeg(ptr, &seg)
		size += 24
	}

	// flush window probing commands
	if (kcp.probe & ASK_TELL) != 0 {
		seg.cmd = CMD_WINS
		if size > int32(kcp.mtu) {
			kcp.output(buffer, size)
			ptr = buffer
			size = 0
		}
		ptr = encodeSeg(ptr, &seg)
		size += 24
	}

	kcp.probe = 0

	// calculate window size
	cwnd = _imin_(kcp.sndWnd, kcp.rmtWnd)
	if kcp.nocwnd == 0 {
		cwnd = _imin_(kcp.cwnd, cwnd)
	}

	// move data from snd_queue to snd_buf
	////println("check",kcp.snd_queue.Len())
	t := 0
	for p := kcp.sndQueue.Front(); p != nil; {
		////println("debug check:", t, p.Next(), kcp.snd_nxt, kcp.snd_una, cwnd, _itimediff(kcp.snd_nxt, kcp.snd_una + cwnd))
		////fmt.Printf("timediff %d,%d,%d,%d\n", kcp.snd_nxt, kcp.snd_una, cwnd, _itimediff(kcp.snd_nxt, kcp.snd_una + cwnd));
		t++
		if _itimediff(kcp.sndNxt, kcp.sndUna+cwnd) >= 0 {
			//if kcp.user[0] == 0 {
			////fmt.Println("=======", kcp.snd_nxt, kcp.snd_una, cwnd)
			//}
			break
		}
		newseg := p.Value.(*IKCPSEG)
		q := p.Next()
		kcp.sndQueue.Remove(p)
		p = q
		kcp.sndBuf.PushBack(newseg)
		//if kcp.user[0] == 0 {
		//println("debug check2:", t, kcp.snd_queue.Len(), kcp.snd_buf.Len(), kcp.nsnd_que)
		//}
		kcp.nsndQue--
		kcp.nsndBuf++

		newseg.conv = kcp.conv
		newseg.cmd = CMD_PUSH
		newseg.wnd = seg.wnd
		newseg.ts = current
		newseg.sn = kcp.sndNxt
		kcp.sndNxt++
		newseg.una = kcp.rcvNxt
		newseg.resendts = current
		newseg.rto = kcp.rxRto
		newseg.fastack = 0
		newseg.xmit = 0
	}

	// calculate resent
	resent = uint32(kcp.fastresend)
	if kcp.fastresend <= 0 {
		resent = 0xffffffff
	}
	rtomin = (kcp.rxRto >> 3)
	if kcp.nodelay != 0 {
		rtomin = 0
	}

	a := 0
	// flush data segments
	for p := kcp.sndBuf.Front(); p != nil; p = p.Next() {
		////println("debug loop", a, kcp.snd_buf.Len())
		a++
		segment := p.Value.(*IKCPSEG)
		needsend := 0
		if segment.xmit == 0 {
			needsend = 1
			segment.xmit++
			segment.rto = kcp.rxRto
			segment.resendts = current + segment.rto + rtomin
		} else if _itimediff(current, segment.resendts) >= 0 {
			needsend = 1
			segment.xmit++
			kcp.xmit++
			if kcp.nodelay == 0 {
				segment.rto += kcp.rxRto
			} else {
				segment.rto += kcp.rxRto / 2
			}
			segment.resendts = current + segment.rto
			lost = 1
		} else if segment.fastack >= resent {
			needsend = 1
			segment.xmit++
			segment.fastack = 0
			segment.resendts = current + segment.rto
			change++
		}
		if needsend != 0 {
			var need int32
			segment.ts = current
			segment.wnd = seg.wnd
			segment.una = kcp.rcvNxt

			need = int32(OVERHEAD + segment._len)

			////fmt.Printf("vzex:need send%d, %d,%d,%d\n", kcp.nsnd_buf, size, need, kcp.mtu)
			if size+need >= int32(kcp.mtu) {
				//      //fmt.Printf("trigger!\n");
				kcp.output(buffer, size)
				ptr = buffer
				size = 0
			}

			ptr = encodeSeg(ptr, segment)
			size += 24

			if segment._len > 0 {
				copy(ptr, segment.data[:segment._len])
				ptr = ptr[segment._len:]
				size += int32(segment._len)
			}

			if segment.xmit >= kcp.deadLink {
				kcp.state = 0
			}
		}
	}

	// flash remain segments
	if size > 0 {
		kcp.output(buffer, size)
	}

	// update ssthresh
	if change != 0 {
		inflight := kcp.sndNxt - kcp.sndUna
		kcp.ssthresh = inflight / 2
		if kcp.ssthresh < THRESH_MIN {
			kcp.ssthresh = THRESH_MIN
		}
		kcp.cwnd = kcp.ssthresh + resent
		kcp.incr = kcp.cwnd * kcp.mss
	}

	if lost != 0 {
		kcp.ssthresh = cwnd / 2
		if kcp.ssthresh < THRESH_MIN {
			kcp.ssthresh = THRESH_MIN
		}
		kcp.cwnd = 1
		kcp.incr = kcp.mss
	}

	if kcp.cwnd < 1 {
		kcp.cwnd = 1
		kcp.incr = kcp.mss
	}
}

// input update
func (kcp *Ikcpcb) Update(current uint32) {
	var slap int32

	kcp.current = current

	if kcp.updated == 0 {
		kcp.updated = 1
		kcp.ts_flush = kcp.current
	}

	slap = _itimediff(kcp.current, kcp.ts_flush)

	if slap >= 10000 || slap < -10000 {
		kcp.ts_flush = kcp.current
		slap = 0
	}

	if slap >= 0 {
		kcp.ts_flush += kcp.interval
		if _itimediff(kcp.current, kcp.ts_flush) >= 0 {
			kcp.ts_flush = kcp.current + kcp.interval
		}
		kcp.Flush()
	}
}

func (kcp *Ikcpcb) Check(current uint32) uint32 {
	ts_flush := kcp.ts_flush
	tm_flush := 0x7fffffff
	tm_packet := 0x7fffffff
	minimal := 0
	if kcp.updated == 0 {
		return current
	}

	if _itimediff(current, ts_flush) >= 10000 ||
		_itimediff(current, ts_flush) < -10000 {
		ts_flush = current
	}

	if _itimediff(current, ts_flush) >= 0 {
		return current
	}

	tm_flush = int(_itimediff(ts_flush, current))

	for p := kcp.sndBuf.Front(); p != nil; p = p.Next() {
		seg := p.Value.(*IKCPSEG)
		diff := _itimediff(seg.resendts, current)
		if diff <= 0 {
			return current
		}
		if diff < int32(tm_packet) {
			tm_packet = int(diff)
		}
	}

	minimal = int(tm_packet)
	if tm_packet >= tm_flush {
		minimal = int(tm_flush)
	}
	if uint32(minimal) >= kcp.interval {
		minimal = int(kcp.interval)
	}

	return current + uint32(minimal)
}

func (kcp *Ikcpcb) Setmtu(mtu int32) int32 {
	if mtu < 50 || mtu < int32(OVERHEAD) {
		return -1
	}
	buffer := make([]byte, (uint32(mtu)+OVERHEAD)*3)
	if buffer == nil {
		return -2
	}
	kcp.mtu = uint32(mtu)
	kcp.mss = kcp.mtu - OVERHEAD
	kcp.buffer = buffer
	return 0
}

func (kcp *Ikcpcb) Interval(interval int32) int32 {
	if interval > 5000 {
		interval = 5000
	} else if interval < 10 {
		interval = 10
	}
	kcp.interval = uint32(interval)
	return 0
}

func (kcp *Ikcpcb) Nodelay(nodelay, interval, resend, nc int32) int32 {
	if nodelay >= 0 {
		kcp.nodelay = uint32(nodelay)
		if nodelay != 0 {
			kcp.rxMinrto = RTO_NDL
		} else {
			kcp.rxMinrto = RTO_MIN
		}
	}
	if interval >= 0 {
		if interval > 5000 {
			interval = 5000
		} else if interval < 10 {
			interval = 10
		}
		kcp.interval = uint32(interval)
	}
	if resend >= 0 {
		kcp.fastresend = resend
	}
	if nc >= 0 {
		kcp.nocwnd = nc
	}
	return 0
}

func (kcp *Ikcpcb) Wndsize(sndwnd, rcvwnd int32) int32 {
	if kcp != nil {
		if sndwnd > 0 {
			kcp.sndWnd = uint32(sndwnd)
		}
		if rcvwnd > 0 {
			kcp.rcvWnd = uint32(rcvwnd)
		}
	}
	return 0
}

func (kcp *Ikcpcb) Waitsnd() int32 {
	return int32(kcp.nsndBuf + kcp.nsndQue)
}
