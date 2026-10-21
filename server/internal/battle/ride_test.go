package battle

import "testing"

// 回复连骑宠一起：按人与骑宠各缺多少的比例分（Magic_RideGetHP），两边都不超过上限；没骑就全给人。
func TestRideHeal(t *testing.T) {
	p := player(0)
	p.HP = p.MaxHP - 20
	p.Ride = &Mount{Name: "m", HP: 20, MaxHP: 100, Atk: 10, Def: 10, Quick: 10}
	ch, pet := p.heal(50) // 缺 20 + 80 = 100：人拿 10、骑宠拿 40
	if ch != 10 || pet != 40 || p.HP != p.MaxHP-10 || p.Ride.HP != 60 {
		t.Fatalf("应按 20:80 分：人 %d 骑宠 %d（hp %d/%d ride %d）", ch, pet, p.HP, p.MaxHP, p.Ride.HP)
	}
	ch, pet = p.heal(1000) // 多出来的不算
	if ch != 10 || pet != 40 || p.HP != p.MaxHP || p.Ride.HP != 100 {
		t.Fatalf("超过上限应截掉：人 %d 骑宠 %d", ch, pet)
	}
	p.Ride.Fell = true
	p.HP -= 30
	if ch, pet = p.heal(50); ch != 30 || pet != 0 {
		t.Fatalf("落马后只回人：人 %d 骑宠 %d", ch, pet)
	}
}
