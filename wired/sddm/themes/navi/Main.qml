import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import QtMultimedia

Rectangle {
    id: root
    color: "black"

    // nightshade palette
    readonly property color neon: "#e879f9"
    readonly property color neonDim: "#a855f7"
    readonly property color ice: "#67e8f9"
    readonly property color ink: "#fafafa"
    readonly property color mute: "#a1a1aa"
    readonly property color panel: "#0b0b10"

    // ---- background: static wallpaper, always there
    Image {
        id: wallpaper
        anchors.fill: parent
        fillMode: Image.PreserveAspectCrop
        source: "file:///usr/share/navi/wired/wp/SElain3.jpg"
    }

    // ---- animated background: drop a muted loop at
    //      /usr/share/navi/wired/wp/login-loop.mp4
    //      (ffmpeg -i navi-lain.gif -movflags +faststart -pix_fmt yuv420p login-loop.mp4)
    //      and it plays over the static image. missing file = static only.
    Video {
        id: motion
        anchors.fill: parent
        fillMode: VideoOutput.PreserveAspectCrop
        source: "file:///usr/share/navi/wired/wp/login-loop.mp4"
        autoPlay: true
        loops: MediaPlayer.Infinite
        muted: true
    }

    // ---- readability veil
    Rectangle {
        anchors.fill: parent
        gradient: Gradient {
            GradientStop { position: 0.0; color: "#a0000000" }
            GradientStop { position: 0.45; color: "#40000000" }
            GradientStop { position: 1.0; color: "#d0000000" }
        }
    }

    // ---- clock (24-hour, DD Month — navi's native conventions)
    ColumnLayout {
        anchors.top: parent.top
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.topMargin: 48
        spacing: 2
        Text {
            id: clock
            Layout.alignment: Qt.AlignHCenter
            color: root.ink
            font.family: "JetBrains Mono"
            font.pixelSize: 64
            font.weight: Font.Light
            text: Qt.formatDateTime(new Date(), "HH:mm")
        }
        Text {
            id: dateline
            Layout.alignment: Qt.AlignHCenter
            color: root.mute
            font.family: "JetBrains Mono"
            font.pixelSize: 16
            text: Qt.formatDateTime(new Date(), "dd MMMM")
        }
        Timer {
            interval: 1000; running: true; repeat: true
            onTriggered: {
                clock.text = Qt.formatDateTime(new Date(), "HH:mm")
                dateline.text = Qt.formatDateTime(new Date(), "dd MMMM")
            }
        }
    }

    // ---- identity + login
    ColumnLayout {
        anchors.centerIn: parent
        spacing: 10

        Text {
            Layout.alignment: Qt.AlignHCenter
            text: "∅"
            color: root.neon
            font.pixelSize: 120
            font.weight: Font.Light
        }
        Text {
            Layout.alignment: Qt.AlignHCenter
            text: "n a v i"
            color: root.ink
            font.family: "JetBrains Mono"
            font.pixelSize: 28
            font.weight: Font.DemiBold
        }
        Text {
            Layout.alignment: Qt.AlignHCenter
            text: "everybody has already entered the wired"
            color: root.mute
            font.family: "JetBrains Mono"
            font.pixelSize: 13
        }

        Item { Layout.preferredHeight: 14 }

        // user picker
        ComboBox {
            id: userBox
            Layout.alignment: Qt.AlignHCenter
            Layout.preferredWidth: 300
            model: userModel
            textRole: "name"
            currentIndex: userModel.lastIndex
            font.family: "JetBrains Mono"
            font.pixelSize: 15
        }

        // password
        TextField {
            id: passwordBox
            Layout.alignment: Qt.AlignHCenter
            Layout.preferredWidth: 300
            font.family: "JetBrains Mono"
            font.pixelSize: 15
            echoMode: TextInput.Password
            passwordCharacter: "•"
            placeholderText: "password"
            placeholderTextColor: root.mute
            focus: true
            onAccepted: loginButton.clicked()
            background: Rectangle {
                color: "#1a1a22"
                border.color: passwordBox.activeFocus ? root.neon : "#3f3f46"
                border.width: 1
                radius: 6
            }
        }

        // session picker: navi (Wayland) / navi (X11)
        ComboBox {
            id: sessionBox
            Layout.alignment: Qt.AlignHCenter
            Layout.preferredWidth: 300
            model: sessionModel
            textRole: "name"
            currentIndex: sessionModel.lastIndex
            font.family: "JetBrains Mono"
            font.pixelSize: 14
        }

        Button {
            id: loginButton
            Layout.alignment: Qt.AlignHCenter
            Layout.preferredWidth: 300
            text: "enter the wired →"
            font.family: "JetBrains Mono"
            font.pixelSize: 15
            font.weight: Font.DemiBold
            onClicked: sddm.login(userBox.currentText, passwordBox.text, sessionBox.currentIndex)
            background: Rectangle {
                color: loginButton.hovered ? root.neonDim : "#27272a"
                border.color: root.neon
                border.width: 1
                radius: 6
            }
            contentItem: Text {
                text: loginButton.text
                color: root.ink
                font: loginButton.font
                horizontalAlignment: Text.AlignHCenter
                verticalAlignment: Text.AlignVCenter
            }
        }

        Text {
            id: failedLabel
            Layout.alignment: Qt.AlignHCenter
            visible: false
            text: "that didn't work — try again"
            color: "#f87171"
            font.family: "JetBrains Mono"
            font.pixelSize: 13
        }

        Connections {
            target: sddm
            function onLoginFailed() {
                failedLabel.visible = true
                passwordBox.clear()
                passwordBox.focus = true
            }
            function onLoginSucceeded() { failedLabel.visible = false }
        }
    }

    // ---- power
    RowLayout {
        anchors.right: parent.right
        anchors.bottom: parent.bottom
        anchors.margins: 24
        spacing: 12
        Button {
            text: "reboot"
            font.family: "JetBrains Mono"
            onClicked: sddm.reboot()
            background: Rectangle { color: "transparent"; border.color: root.mute; radius: 6 }
            contentItem: Text { text: "reboot"; color: root.mute; font.family: "JetBrains Mono"; horizontalAlignment: Text.AlignHCenter }
        }
        Button {
            text: "shut down"
            font.family: "JetBrains Mono"
            onClicked: sddm.powerOff()
            background: Rectangle { color: "transparent"; border.color: root.mute; radius: 6 }
            contentItem: Text { text: "shut down"; color: root.mute; font.family: "JetBrains Mono"; horizontalAlignment: Text.AlignHCenter }
        }
    }

    // ---- footer
    Text {
        anchors.left: parent.left
        anchors.bottom: parent.bottom
        anchors.margins: 24
        text: "navi 1.2 \"mika\""
        color: root.mute
        font.family: "JetBrains Mono"
        font.pixelSize: 12
    }
}
