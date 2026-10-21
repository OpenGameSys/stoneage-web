package world

import (
	"strings"
	"testing"

	"sa/server/internal/proto"
)

// 任务日志（jobdaily.txt）：上线时送；旗标改了（GM /event、任务交换）就重送；条件照 NOWEV / ENDEV 与 "!=" 判断；
// 带相关 NPC 的提示。
func TestQuestLog(t *testing.T) {
	w, db, g := setup(t)
	if len(g.Quests) < 100 {
		t.Fatalf("jobdaily.txt 应有上百行，读到 %d", len(g.Quests))
	}
	w.GMs = map[string]bool{"quester": true}
	c := newChar(t, db, "quester", 1000, 47, 82)
	cl := &gmClient{account: "quester"}
	w.Enter(c, cl)
	w.Do(func() {})
	q, ok := find[*proto.QuestLog](cl.take())
	if !ok {
		t.Fatal("上线应收到任务日志")
	}
	if len(q.Quests) != 0 {
		t.Fatalf("新角色不该有任务：%v", q.Quests)
	}
	has := func(list []proto.QuestEntry, id int, state string) *proto.QuestEntry {
		for i := range list {
			if list[i].ID == id && list[i].State == state {
				return &list[i]
			}
		}
		return nil
	}
	// 事件 7 进行中（哥亚山小猪传情任务(二)：NOWEV=7&ENDEV!=8 …）
	w.GmCommand(int(c.ID), "/event 7 now")
	q, ok = find[*proto.QuestLog](cl.take())
	if !ok {
		t.Fatal("旗标改了应重送任务日志")
	}
	e := has(q.Quests, 7, "进行中")
	if e == nil {
		t.Fatalf("事件 7 进行中应列出来：%v", q.Quests)
	}
	if e.Hint == "" {
		t.Errorf("任务 7 应有相关 NPC 的提示")
	}
	// 事件 8 做完 → 7 变成已完成（ENDEV=8），进行中的那行不再成立（ENDEV!=8）
	w.GmCommand(int(c.ID), "/event 8 done")
	q, ok = find[*proto.QuestLog](cl.take())
	if !ok {
		t.Fatal("旗标改了应重送任务日志")
	}
	if has(q.Quests, 7, "进行中") != nil || has(q.Quests, 7, "已完成") == nil {
		t.Fatalf("事件 8 做完后任务 7 应是已完成：%v", q.Quests)
	}
	// 没改旗标的指令不重送；客户端要（QuestReq）就送
	w.GmCommand(int(c.ID), "/where")
	if _, ok := find[*proto.QuestLog](cl.take()); ok {
		t.Error("旗标没改不该重送任务日志")
	}
	w.QuestReq(int(c.ID))
	if _, ok := find[*proto.QuestLog](cl.take()); !ok {
		t.Error("QuestReq 应回任务日志")
	}
	// 用道具记进度的任务（jobdaily_h5.txt 的 JOT）：拿到检查证明石 2552 就列出 A 路线第 1 站，提示指向检查人员(A-1)；旗标没改也要重送
	w.GmCommand(int(c.ID), "/item 2552")
	q, ok = find[*proto.QuestLog](cl.take())
	if !ok {
		t.Fatal("拿到任务道具应重送任务日志")
	}
	e = has(q.Quests, 9001, "进行中")
	if e == nil || !strings.Contains(e.Name, "第1检查站") || !strings.Contains(e.Hint, "检查人员(A-1)") {
		t.Fatalf("JOT 第 1 站应列出来并指向检查人员(A-1)：%+v", q.Quests)
	}
}
