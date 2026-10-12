import QtQuick
import qs.Commons

Item {
  id: hint

  required property Theme theme
  property string keys: ""
  property string label: ""

  signal clicked()

  implicitWidth: hintRow.implicitWidth
  implicitHeight: hintRow.implicitHeight

  Row {
    id: hintRow
    spacing: Style.space(4)

    Repeater {
      model: hint.keys.split(" ")
      Kbd {
        required property var modelData
        theme: hint.theme
        text: modelData
      }
    }

    PanelText {
      theme: hint.theme
      anchors.verticalCenter: parent.verticalCenter
      text: hint.label
      color: hintMouse.containsMouse ? hint.theme.foreground : hint.theme.dim
    }
  }

  MouseArea {
    id: hintMouse
    anchors.fill: parent
    hoverEnabled: true
    cursorShape: Qt.PointingHandCursor
    onClicked: hint.clicked()
  }
}
