package harness

import (
	"fmt"
	"time"

	"eucli-deep-wiki-contract-guard/contract"
)

// HoldSyncGate 让服务端真的处在一轮同步里，从而占住它的破坏性操作闸门。
//
// 做法不是「假装正忙」：它触发一轮真实的全量同步，而现场里有一个远端指向黑障
// 的仓库——那一轮同步会卡在真实网络握手上，闸门因此被真实占住。等到状态接口
// 自己报告正在运行，才返回；释放时断掉黑障连接，那一轮同步立刻失败收场，
// 闸门随之放开。
func (world *World) HoldSyncGate() (release func(), err error) {
	if world.Hole == nil {
		return nil, fmt.Errorf("这个现场没有黑障，无法占住同步闸门")
	}
	add := world.PostJSON("/v1/repos", nil, map[string]any{
		"urls":   []string{world.Hole.HTTPSURL(contract.RepoGitBlackHole)},
		"reload": true,
	})
	if add.Status != 200 {
		return nil, fmt.Errorf("登记黑障仓库失败：%s", add.Describe())
	}

	syncAll := world.Post("/v1/sync", nil)
	if syncAll.Status != 202 {
		return nil, fmt.Errorf("触发全量同步失败：%s", syncAll.Describe())
	}
	if err := world.waitSyncRunning(true, 30*time.Second); err != nil {
		world.Hole.Drop()
		return nil, err
	}

	return func() {
		world.Hole.Drop()
		_ = world.waitSyncRunning(false, 60*time.Second)
	}, nil
}

// waitSyncRunning 等到状态接口报告「正在运行」为 want。
func (world *World) waitSyncRunning(want bool, within time.Duration) error {
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		message := world.Get("/status", nil)
		if message.Status == 200 {
			if running, ok := message.BoolField("running"); ok && running == want {
				return nil
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	if want {
		return fmt.Errorf("服务端在 %s 内没有进入运行状态", within)
	}
	return fmt.Errorf("服务端在 %s 内没有结束这一轮同步", within)
}
