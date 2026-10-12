import QtQuick
import qs.Ui

PanelActionButton {
  required property Theme theme

  fontFamily: theme.fontFamily
  foreground: theme.faint
  hoverColor: theme.foreground
  radius: theme.controlRadius
}
