package world

import (
	"slices"
	"strconv"
	"strings"
)

// 任务旗标（npcutil.c NPC_EventCheckFlg / NPC_NowEventCheckFlg 等）：每个事件编号一位，"做完的"（CHAR_ENDEVENT..）与
// "进行中的"（CHAR_NOWEVENT..）各一组，原版 10 个 int 共 320 位。存档是逗号分开的编号（store.Character.EndEvents / NowEvents）。

const maxEventFlag = 32 * 10

func parseFlags(s string) []int {
	var out []int
	for _, f := range strings.Split(s, ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(f)); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func flagsString(list []int) string {
	slices.Sort(list)
	list = slices.Compact(list)
	parts := make([]string, len(list))
	for i, n := range list {
		parts[i] = strconv.Itoa(n)
	}
	return strings.Join(parts, ",")
}

func hasFlag(s string, bit int) bool {
	return bit >= 0 && slices.Contains(parseFlags(s), bit)
}

func setFlag(s *string, bit int, on bool) {
	if bit < 0 || bit >= maxEventFlag {
		return
	}
	list := slices.DeleteFunc(parseFlags(*s), func(n int) bool { return n == bit })
	if on {
		list = append(list, bit)
	}
	*s = flagsString(list)
}

// eventDone 是 NPC_EventCheckFlg：事件做完了没有（−1 一律否）。
func (p *entity) eventDone(bit int) bool { return hasFlag(p.char.EndEvents, bit) }

// eventNow 是 NPC_NowEventCheckFlg：事件是否进行中。
func (p *entity) eventNow(bit int) bool { return hasFlag(p.char.NowEvents, bit) }

// setEventDone 是 NPC_EventSetFlg（做完）；setEventNow 是 NPC_NowEventSetFlg（开始）；
// clearEventNow 是 NPC_NowEventSetFlgCls；clearEvent 是 NPC_NowEndEventSetFlgCls（两组都清掉）。
func (p *entity) setEventDone(bit int)  { setFlag(&p.char.EndEvents, bit, true) }
func (p *entity) setEventNow(bit int)   { setFlag(&p.char.NowEvents, bit, true) }
func (p *entity) clearEventNow(bit int) { setFlag(&p.char.NowEvents, bit, false) }
func (p *entity) clearEvent(bit int) {
	setFlag(&p.char.NowEvents, bit, false)
	setFlag(&p.char.EndEvents, bit, false)
}
