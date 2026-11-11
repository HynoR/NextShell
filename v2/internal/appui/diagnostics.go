package appui

import "github.com/egoist/mygo/ui"

func (a *App) diagnosticsView(c *ui.Context) {
	s := &a.Phase2
	a.phasePage(c, "网络诊断", func() {
		ui.Text(c, "目标仅接收IP；默认localhost，点击才开始。给CLI传禁用GeoIP/RDNS/地图的参数；用户安装版本的实际行为需另验。").TextColor(c.Theme().TextMuted)
		field(c, "诊断目标IP", &s.DiagnosticTarget)
		ui.Row(c).Gap(8).Children(func() {
			if ui.Button(c, "Ping（pro-bing）").Disabled(s.DiagnosticBusy).Clicked() {
				s.DiagnosticID = a.P.Diagnose("ping", s.DiagnosticTarget)
				s.DiagnosticBusy = s.DiagnosticID != ""
				s.DiagnosticText = "正在Ping…"
			}
			if ui.Button(c, "Traceroute（用户nexttrace）").Disabled(s.DiagnosticBusy).Clicked() {
				s.DiagnosticID = a.P.Diagnose("traceroute", s.DiagnosticTarget)
				s.DiagnosticBusy = s.DiagnosticID != ""
				s.DiagnosticText = "正在调用用户安装的nexttrace…"
			}
			if ui.Button(c, "停止诊断").Disabled(!s.DiagnosticBusy).Clicked() {
				a.P.CancelDiagnostic()
			}
		})
		ui.Text(c, "Ping需系统允许非特权ICMP socket；权限不足会明确报错，不请求提权。Traceroute通过设置页指定程序，默认PATH查找nexttrace；不捆绑/链接GPL代码。旧CLI不识别参数时直接报错，不重试。")
		if s.DiagnosticBusy {
			ui.Progress(c, -1).FillWidth()
		}
		ui.Text(c, s.DiagnosticText).Selectable().FillWidth()
	})
}
