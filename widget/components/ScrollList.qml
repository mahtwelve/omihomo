import QtQuick
import QtQuick.Controls
import qs.Commons

ListView {
  id: scrollList

  required property Theme theme
  readonly property bool scrollable: contentHeight > height
  readonly property real rowWidth: width - (scrollable ? Style.space(10) : 0)

  clip: true
  boundsBehavior: Flickable.StopAtBounds
  interactive: scrollable
  ScrollBar.vertical: ScrollBar { policy: scrollList.scrollable ? ScrollBar.AlwaysOn : ScrollBar.AsNeeded }

  Rectangle {
    visible: scrollList.scrollable && !scrollList.atYBeginning
    parent: scrollList
    anchors.top: parent.top
    anchors.left: parent.left
    width: scrollList.rowWidth
    height: Style.space(18)
    z: 2
    gradient: Gradient {
      GradientStop { position: 0; color: scrollList.theme.surface }
      GradientStop { position: 1; color: Util.alpha(scrollList.theme.surface, 0) }
    }
  }

  Rectangle {
    visible: scrollList.scrollable && !scrollList.atYEnd
    parent: scrollList
    anchors.bottom: parent.bottom
    anchors.left: parent.left
    width: scrollList.rowWidth
    height: Style.space(18)
    z: 2
    gradient: Gradient {
      GradientStop { position: 0; color: Util.alpha(scrollList.theme.surface, 0) }
      GradientStop { position: 1; color: scrollList.theme.surface }
    }
  }
}
