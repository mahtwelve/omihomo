import QtQuick
import QtQuick.Layouts
import qs.Commons
import "../components"

NavRow {
  id: targetRow

  property var target: ({})

  section: "target"
  current: targetRow.target.value === panel.ruleTarget
  implicitHeight: Style.space(26)
  onActivated: panel.pickTargetAt(targetRow.rowIndex)

  RowLayout {
    anchors.fill: parent
    anchors.leftMargin: Style.space(10)
    anchors.rightMargin: Style.space(10)
    spacing: Style.space(8)

    NameText {
      theme: targetRow.theme
      Layout.fillWidth: true
      text: targetRow.target.label || ""
      bold: targetRow.current
    }
    PanelText {
      theme: targetRow.theme
      text: targetRow.target.description || ""
      color: targetRow.theme.faint
    }
  }
}
