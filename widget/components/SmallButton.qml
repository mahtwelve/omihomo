import QtQuick
import qs.Commons
import qs.Ui

Button {
  required property Theme theme
  property bool ghost: false
  property bool primary: false
  property bool danger: false

  bordered: true
  foreground: danger ? theme.urgent : (primary ? theme.surface : theme.foreground)
  fontFamily: theme.fontFamily
  fontSize: Style.font.bodySmall
  iconSize: Style.font.body
  horizontalPadding: Style.space(9)
  verticalPadding: Style.space(3)
  radius: theme.controlRadius
  color: primary ? theme.accent : (hot ? theme.hoverFill : (ghost || danger ? "transparent" : theme.fill))
  borderSpec: Border.flat(hasCursor ? theme.accent : (danger ? Util.alpha(theme.urgent, 0.55) : (ghost ? "transparent" : (primary ? theme.accent : theme.outline))), hasCursor ? 2 : 1)
  implicitHeight: Style.space(24)
  opacity: enabled ? 1 : 0.4
}
