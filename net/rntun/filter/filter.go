package filter

type Response int

// todo (snt) fix
const (
	Drop         Response = iota // パケット処理を続行しない。
	DropSilently                 // パケットの処理を続行しないが、ログも記録しない。
	Accept                       // パケット処理を続行する。
	noVerdict                    // まだ判定がないため、フィルタの実行を続行する。
)
