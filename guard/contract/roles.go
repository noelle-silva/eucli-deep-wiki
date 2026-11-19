package contract

// 现场里固定角色的仓库名。它们同时是验证现场铺盘的依据与被比较的期望值，
// 因此只能有一处出处：harness 直接用这些常量铺盘，用例用它们断言。
const (
	// RepoOnDisk 在名单里、磁盘上有副本、声明档位 shallow、被体积保护拦住。
	RepoOnDisk = "alpha"
	// RepoDeclaredOnly 在名单里、磁盘上没有副本、未声明档位。
	RepoDeclaredOnly = "beta"
	// RepoFullHistory 在名单里、磁盘上有副本、声明档位 full。
	RepoFullHistory = "gamma"
	// RepoAbsent 不在名单里，用来验证「仓库不存在」这一路。
	RepoAbsent = "missing"
	// RepoToAdd 不在名单里且是合法地址末段，用来验证「加入名单」。
	RepoToAdd = "delta"
)

// 真实版本管理现场里的仓库角色：它们由真实 git 命令做出来，
// 因此瘦身的四种动作与几种失败都能被真实代码路径走到。
const (
	// RepoGitFull 是完整克隆，名单声明 shallow：预演说会转换，执行会真的转换。
	RepoGitFull = "full"
	// RepoGitShallow 是真浅克隆：动作是「本来就是浅克隆」。
	RepoGitShallow = "shallow-clone"
	// RepoGitFullHistory 是完整克隆且名单声明 full：动作是「因要求完整历史而跳过」。
	RepoGitFullHistory = "full-history"
	// RepoGitDetached 停在游离头指针上。
	RepoGitDetached = "detached"
	// RepoGitBadOrigin 的 origin 指向一个不存在的仓库。
	RepoGitBadOrigin = "bad-origin"
	// RepoGitLockedConfig 的 .git/config 被锁住：写配置这一步必然失败。
	RepoGitLockedConfig = "locked-config"
	// RepoGitBlackHole 的远端是一个接了电话不说话的黑障：它的网络动作会卡在握手上。
	RepoGitBlackHole = "black-hole"
)
