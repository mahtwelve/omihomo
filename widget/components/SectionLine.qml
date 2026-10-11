import QtQuick
import QtQuick.Layouts
import qs.Commons

RowLayout {
  id: sectionLine

  required property Theme theme
  property string title: ""
  property string note: ""

  spacing: Style.space(8)

  PanelText {
    theme: sectionLine.theme
    text: sectionLine.title
    color: sectionLine.theme.dim
    font.letterSpacing: Style.space(0.8)
  }

  Item { Layout.fillWidth: true }

  PanelText {
    theme: sectionLine.theme
    visible: text !== ""
    text: sectionLine.note
    color: sectionLine.theme.faint
  }
}
