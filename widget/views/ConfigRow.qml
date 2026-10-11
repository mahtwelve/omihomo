import QtQuick
import QtQuick.Layouts
import qs.Commons
import "../components"
import "../Model.js" as Model

NavRow {
  id: configRow

  property string configName: ""
  readonly property bool selected: panel.browsedGroupEntry !== null && panel.browsedGroupEntry.now === configRow.configName
  readonly property bool pending: panel.service.pendingConfig === configRow.configName
  readonly property string testState: panel.service.configTestState(configRow.configName)
  readonly property int delay: panel.service.configDelay(configRow.configName)

  section: "config"
  current: configRow.selected
  pendingChoice: configRow.pending && !configRow.selected
  implicitHeight: Style.space(30)
  onActivated: panel.chooseConfigAt(configRow.rowIndex)

  RowLayout {
    anchors.fill: parent
    anchors.leftMargin: Style.space(10)
    anchors.rightMargin: Style.space(4)
    spacing: Style.space(8)

    NameText {
      theme: configRow.theme
      Layout.fillWidth: true
      text: configRow.configName
      pixelSize: Style.font.body
      bold: configRow.selected
    }
    Rectangle {
      id: pill

      readonly property bool failed: configRow.testState === "failed"
      readonly property bool testing: configRow.testState === "testing"

      implicitWidth: Math.max(pillText.implicitWidth + Style.space(12), Style.space(52))
      implicitHeight: pillText.implicitHeight + Style.space(2)
      radius: Math.min(configRow.theme.controlRadius, Style.space(5))
      color: pill.failed ? configRow.theme.urgent
        : (configRow.delay > 0 && !pill.testing
          ? configRow.theme.latencyFill(Model.delayHeat(configRow.delay, configRow.panel.delaySpan))
          : "transparent")

      PanelText {
        id: pillText
        theme: configRow.theme
        anchors.centerIn: parent
        text: pill.testing ? "···" : (pill.failed ? "timeout" : Model.formatDelay(configRow.delay))
        color: pill.failed ? configRow.theme.surface : (configRow.delay > 0 ? configRow.theme.foreground : configRow.theme.faint)

        SequentialAnimation on opacity {
          running: pill.testing
          loops: Animation.Infinite
          alwaysRunToEnd: true
          NumberAnimation { from: 1; to: 0.3; duration: 450 }
          NumberAnimation { from: 0.3; to: 1; duration: 450 }
        }
      }
    }

    InlineAction {
      theme: configRow.theme
      iconText: "󰓅"
      tooltipText: "Test latency · d"
      size: Style.space(22)
      enabled: !configRow.panel.service.pingTestsRunning
      onClicked: configRow.panel.service.testConfig(configRow.configName)
    }
  }
}
