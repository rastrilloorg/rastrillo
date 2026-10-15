package alias

import u "amadan.net/rastrillo/rastrillo/ui"

func wire() {
	_ = u.WithT(nil)
	_ = u.FuncsWith(nil)
	f := u.Funcs()
	f["Tbdi"] = nil
}
