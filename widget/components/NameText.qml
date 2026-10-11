import QtQuick
import QtQuick.Layouts
import qs.Commons
import "../Model.js" as Model

RowLayout {
  id: name

  required property Theme theme
  property string text: ""
  property color color: theme.foreground
  property int pixelSize: Style.font.bodySmall
  property bool bold: false
  readonly property var parts: Model.leadingEmoji(text)

  spacing: markSlot.advanceWidth / 2

  TextMetrics {
    id: markSlot
    font.family: name.theme.fontFamily
    font.pixelSize: name.pixelSize
    text: "MM"
  }

  PanelText {
    theme: name.theme
    visible: name.parts.mark !== ""
    Layout.minimumWidth: markSlot.advanceWidth
    Layout.maximumWidth: markSlot.advanceWidth
    text: name.parts.mark
    color: name.color
    font.pixelSize: name.pixelSize
  }
  PanelText {
    id: nameRest
    theme: name.theme
    Layout.fillWidth: true
    text: name.parts.rest
    color: name.color
    font.pixelSize: name.pixelSize
    font.bold: name.bold
    elide: Text.ElideRight
  }
}
