import QtQuick
import qs.Commons
import qs.Ui

CursorSurface {
  id: navRow

  required property var panel
  readonly property Theme theme: panel.theme
  property string section: ""
  property int rowIndex: -1
  property bool pendingChoice: false
  property color restFill: "transparent"
  readonly property alias hovered: navMouse.containsMouse
  readonly property color fadeFill: current ? Util.alpha(theme.accent, hasCursor ? 0.4 : 0.28)
    : (pendingChoice ? Util.alpha(theme.accent, hasCursor ? 0.16 : 0.08) : Util.alpha(theme.foreground, 0.1))

  signal activated()

  hasCursor: panel.at(navRow.section, navRow.rowIndex < 0 ? undefined : navRow.rowIndex)
  foreground: theme.foreground
  radius: theme.controlRadius
  borderSpec: Border.none()
  color: restFill
  gradient: current || pendingChoice || hasCursor ? fade : null

  Gradient {
    id: fade
    orientation: Gradient.Horizontal
    GradientStop { position: 0; color: navRow.fadeFill }
    GradientStop { position: 1; color: navRow.restFill.a > 0 ? navRow.restFill : Util.alpha(navRow.fadeFill, 0) }
  }

  MouseArea {
    id: navMouse
    anchors.fill: parent
    hoverEnabled: true
    cursorShape: Qt.PointingHandCursor
    onContainsMouseChanged: if (containsMouse) navRow.panel.focusRow(navRow.section,
      navRow.rowIndex < 0 ? undefined : navRow.rowIndex)
    onClicked: navRow.activated()
  }
}
