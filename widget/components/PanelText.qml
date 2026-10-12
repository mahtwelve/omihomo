import QtQuick
import qs.Commons

Text {
  required property Theme theme

  renderType: Text.NativeRendering
  textFormat: Text.PlainText
  font.family: theme.fontFamily
  font.pixelSize: Style.font.bodySmall
}
