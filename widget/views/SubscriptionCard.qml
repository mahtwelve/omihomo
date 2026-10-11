import QtQuick
import QtQuick.Layouts
import qs.Commons
import "../components"
import "../Model.js" as Model

NavRow {
  id: card

  property var subscription: null
  readonly property string subName: subscription ? String(subscription.name) : ""
  readonly property bool active: !!subscription && subscription.active === true
  readonly property bool pending: subName !== "" && panel.service.pendingSubscription === subName
  readonly property bool failed: panel.errorAt("sub:" + subName)
  readonly property var usage: Model.subscriptionUsage(subscription)
  readonly property string quotaText: usage.text.replace(/(\S+) (\S+) of (\S+) \2/, "$1/$3 $2").replace("expires ", "exp ")

  section: "sub"
  current: active
  pendingChoice: card.pending && !card.active
  radius: theme.cardRadius
  restFill: theme.fill
  borderSpec: Border.flat(failed ? theme.urgent : theme.line, 1)
  implicitHeight: cardColumn.implicitHeight + Style.space(18)
  onActivated: panel.activateSubscriptionAt(card.rowIndex)

  ColumnLayout {
    id: cardColumn
    anchors.left: parent.left
    anchors.right: parent.right
    anchors.verticalCenter: parent.verticalCenter
    anchors.leftMargin: Style.space(10)
    anchors.rightMargin: Style.space(10)
    spacing: Style.space(7)

    RowLayout {
      Layout.fillWidth: true
      spacing: Style.space(8)

      PanelText {
        theme: card.theme
        Layout.fillWidth: true
        text: card.subName
        color: card.theme.foreground
        font.pixelSize: Style.font.body
        font.bold: card.active
        elide: Text.ElideRight
      }

      Tag {
        theme: card.theme
        text: card.pending ? "working…" : (card.failed ? "failed" : (card.active ? "active"
          : "updated " + (Model.relativeTime(card.subscription ? card.subscription.updatedAt : "", card.panel.nowMs) || "never")))
        tint: card.failed ? card.theme.urgent : (card.active ? card.theme.accent : card.theme.dim)
        strong: card.active || card.failed
        pill: false
      }

      SmallButton {
        theme: card.theme
        text: "update"
        iconText: "󰑐"
        iconSpinning: card.pending
        enabled: !card.pending
        tooltipText: "Update · u"
        onClicked: card.panel.service.updateSubscription(card.subName)
      }

      InlineAction {
        theme: card.theme
        iconText: "×"
        tooltipText: "Remove · x"
        foreground: card.theme.dim
        hoverColor: card.theme.urgent
        size: Style.space(18)
        onClicked: card.panel.removeSubscription(card.subName)
      }
    }

    RowLayout {
      Layout.fillWidth: true
      spacing: Style.space(8)

      Rectangle {
        Layout.fillWidth: true
        visible: card.usage.fraction >= 0
        Layout.minimumWidth: Style.space(36)
        implicitHeight: Style.space(5)
        radius: Math.min(Style.cornerRadius, 2)
        color: Util.alpha(card.theme.foreground, 0.12)

        Rectangle {
          width: parent.width * Math.max(0, card.usage.fraction)
          height: parent.height
          radius: parent.radius
          color: card.usage.fraction > 0.9 ? card.theme.urgent : (card.active ? card.theme.accent : card.theme.faint)
        }
      }

      PanelText {
        theme: card.theme
        Layout.maximumWidth: cardColumn.width - Style.space(64)
        text: card.quotaText + (card.active ? " · "
          + (Model.relativeTime(card.subscription ? card.subscription.updatedAt : "", card.panel.nowMs) || "never") : "")
        color: card.theme.dim
        elide: Text.ElideRight
      }
    }

    Rectangle {
      visible: card.failed
      Layout.fillWidth: true
      implicitHeight: Math.max(errorMessage.implicitHeight, Style.space(24)) + Style.space(14)
      radius: card.theme.controlRadius
      color: Util.alpha(card.theme.urgent, 0.14)
      Rectangle { anchors.left: parent.left; height: parent.height; width: Style.space(3); color: card.theme.urgent }

      RowLayout {
        id: errorRow
        anchors.fill: parent
        anchors.margins: Style.space(7)
        spacing: Style.space(8)

        PanelText {
          id: errorMessage
          theme: card.theme
          Layout.fillWidth: true
          text: "▲ " + card.panel.service.lastError
          color: card.theme.urgent
          wrapMode: Text.WordWrap
        }

        SmallButton {
          theme: card.theme
          text: "retry"
          iconText: "󰑐"
          primary: true
          onClicked: card.panel.service.updateSubscription(card.subName)
        }
      }
    }

    RowLayout {
      visible: card.panel.removeArmed === card.subName
      Layout.fillWidth: true
      spacing: Style.space(6)

      PanelText {
        theme: card.theme
        Layout.fillWidth: true
        text: "Remove " + card.subName + "?"
        color: card.theme.foreground
        elide: Text.ElideRight
      }

      SmallButton {
        theme: card.theme
        text: "keep"
        onClicked: card.panel.removeArmed = ""
      }

      SmallButton {
        theme: card.theme
        text: "remove · x"
        foreground: card.theme.urgent
        onClicked: card.panel.removeSubscription(card.subName)
      }
    }
  }
}
