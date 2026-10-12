import QtQuick
import qs.Commons
import qs.Ui
import "../components"

NavRow {
  id: chip

  property var group: null
  readonly property string groupName: group ? String(group.name) : ""
  readonly property bool primary: groupName !== "" && groupName === panel.service.primaryGroup

  section: "group"
  current: groupName === panel.browsedGroup
  bordered: true
  implicitWidth: chipRow.implicitWidth + Style.space(20)
  implicitHeight: chipRow.implicitHeight + Style.space(8)
  radius: Style.cornerRadius > 0 ? height / 2 : 0
  gradient: null
  color: current ? Util.alpha(theme.accent, hasCursor ? 0.24 : 0.16) : (hasCursor ? Util.alpha(theme.foreground, 0.09) : "transparent")
  borderSpec: Border.flat(current ? Util.alpha(theme.accent, 0.5) : theme.outline, 1)
  onActivated: panel.browseGroupAt(chip.rowIndex)

  Row {
    id: chipRow
    anchors.centerIn: parent
    spacing: Style.space(6)

    PanelText {
      theme: chip.theme
      anchors.verticalCenter: parent.verticalCenter
      text: chip.groupName
      color: chip.current || chip.hasCursor ? chip.theme.foreground : chip.theme.dim
    }

    Rectangle {
      visible: chip.primary
      anchors.verticalCenter: parent.verticalCenter
      width: Style.space(5)
      height: width
      radius: width / 2
      color: chip.theme.accent
    }
  }

  PanelToolTip {
    visible: chip.hovered
    text: chip.primary ? "Primary group" : "Browse · p makes it primary"
    fontFamily: chip.theme.fontFamily
  }
}
