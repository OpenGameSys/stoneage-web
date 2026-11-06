package data

import (
	"os"
	"path/filepath"
	"strings"
)

// petTalkMax 是原版 PETTALK_MAXID（_PET_TALKPRO）：最多读几只宠物的对话。
const petTalkMax = 8

// LoadPetTalk 是 configfile.c LoadPetTalk（_PET_TALKPRO）：npc/pettalk/pettalk.menu 各行以 "|" 接起来、按 "END" 分段，
// 每段的 PETTEMPNO:档名,模板编号 指定一只宠物的对话档；对话档各行同样以 "|" 接起来。回传 模板编号 → 对话内容。
// 读不到的档案略过（原版印出错误、这一项不算）。
func LoadPetTalk(npcDir string) map[int]string {
	out := map[int]string{}
	b, err := os.ReadFile(filepath.Join(npcDir, "pettalk", "pettalk.menu"))
	if err != nil {
		return out
	}
	for _, seg := range strings.Split(joinLines(GBK(b)), "END") {
		v, ok := ArgField(seg, "PETTEMPNO")
		if !ok {
			continue
		}
		file, id, ok := strings.Cut(v, ",")
		if !ok {
			continue
		}
		body, err := os.ReadFile(filepath.Join(npcDir, "pettalk", file))
		if err != nil {
			continue
		}
		out[Atoi(id)] = joinLines(GBK(body))
		if len(out) >= petTalkMax {
			break
		}
	}
	return out
}

// joinLines 照原版读档：每行去掉行尾换行（chompex），前面累积的内容不是以 "|" 结尾时先补一个 "|"。
func joinLines(s string) string {
	var sb strings.Builder
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r", ""), "\n") {
		if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "|") {
			sb.WriteByte('|')
		}
		sb.WriteString(line)
	}
	return sb.String()
}
