import QtQuick
import qs.Commons
import qs.Ui

BorderSurface {
  id: kbd

  required property Theme theme
  property string text: ""

  implicitWidth: Math.max(kbdText.implicitWidth + Style.space(8), Style.space(18))
  implicitHeight: Style.space(18)
  radius: Math.min(Style.cornerRadius, 4)
  color: Util.alpha(theme.foreground, 0.06)
  borderSpec: Border.flat(Util.alpha(theme.foreground, 0.2), "1 1 2 1")

  PanelText {
    id: kbdText
    theme: kbd.theme
    anchors.centerIn: parent
    anchors.verticalCenterOffset: -0.5
    text: kbd.text
    color: kbd.theme.foreground
  }
}
