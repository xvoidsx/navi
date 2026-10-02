{
  description = "tachibana — navi's NixOS experiment (ISO stage)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs, ... }: {
    nixosConfigurations.tachibana = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        "${nixpkgs}/nixos/modules/installer/cd-dvd/iso-image.nix"
        ({ config, pkgs, lib, ... }: {
          # --- ISO identity ---
          isoImage.isoName = "tachibana-experimental.iso";
          isoImage.volumeID = "TACHIBANA";
          isoImage.makeEfiBootable = true;
          isoImage.makeUsbBootable = true;

          # --- boot ---
          boot.loader.systemd-boot.enable = true;
          boot.loader.efi.canTouchEfiVariables = true;

          # --- identity ---
          networking.hostName = "tachibana";
          networking.networkmanager.enable = true;
          time.timeZone = "America/Chicago";
          i18n.defaultLocale = "en_US.UTF-8";

          # --- live user (ISO boots straight in) ---
          users.users.navi = {
            isNormalUser = true;
            extraGroups = [ "wheel" "networkmanager" "video" "audio" ];
            initialPassword = "navi";
          };
          security.sudo.wheelNeedsPassword = false;

          # --- autologin to sway for the live ISO ---
          services.displayManager.autoLogin.enable = true;
          services.displayManager.autoLogin.user = "navi";

          # --- the wired: sway ---
          programs.sway = {
            enable = true;
            wrapperFeatures.gtk = true;
            extraPackages = with pkgs; [
              swaylock
              swayidle
              wl-clipboard
              mako # notifications (dunst later)
            ];
          };

          # --- waybar with navi's real config ---
          programs.waybar.enable = true;

          # navi's waybar config + theme, straight from the repo.
          # Custom modules will fail gracefully until their scripts are packaged —
          # that failure IS the FHS audit.
          environment.etc."xdg/waybar/config".source = ./wired/waybar/config.jsonc;
          environment.etc."xdg/waybar/style.css".source = ./wired/waybar/style.css;

          # --- terminals ---
          environment.systemPackages = with pkgs; [
            alacritty
            foot
            rofi
            wofi
            grim # screenshots
            slurp # region select
          ];

          # --- sound ---
          services.pipewire = {
            enable = true;
            alsa.enable = true;
            pulse.enable = true;
          };

          # --- firmware (Cloudbook needs it) ---
          hardware.enableAllFirmware = true;

          # --- SSH for debugging ---
          services.openssh.enable = true;

          # --- minimal navi-flavored sway config ---
          # NOT the full wired config yet — that has 100+ FHS-dependent
          # exec lines. We layer it in piece by piece after base boots.
          environment.etc."sway/config".text = ''
            # tachibana minimal sway — navi flavor, NixOS base
            set $mod Mod4
            set $term alacritty

            # waybar
            bar {
              swaybar_command waybar
            }

            # basics
            bindsym $mod+Return exec $term
            bindsym $mod+d exec rofi -show drun
            bindsym $mod+Shift+q kill
            bindsym $mod+Shift+e exec swaynag -t warning -m 'Exit?' -b 'Yes' 'swaymsg exit'

            # focus
            bindsym $mod+h focus left
            bindsym $mod+j focus down
            bindsym $mod+k focus up
            bindsym $mod+l focus right

            # move
            bindsym $mod+Shift+h move left
            bindsym $mod+Shift+j move down
            bindsym $mod+Shift+k move up
            bindsym $mod+Shift+l move right

            # workspaces
            bindsym $mod+1 workspace number 1
            bindsym $mod+2 workspace number 2
            bindsym $mod+3 workspace number 3
            bindsym $mod+4 workspace number 4
            bindsym $mod+Shift+1 move container to workspace number 1
            bindsym $mod+Shift+2 move container to workspace number 2
            bindsym $mod+Shift+3 move container to workspace number 3
            bindsym $mod+Shift+4 move container to workspace number 4

            # layout
            bindsym $mod+b splith
            bindsym $mod+v splitv
            bindsym $mod+f fullscreen toggle
            bindsym $mod+space floating toggle

            # reload
            bindsym $mod+Shift+c reload

            # output
            output * bg #0f0f0f solid_color
          '';

          # --- greeter ---
          services.displayManager.sddm.enable = true;

          system.stateVersion = "25.05";
        })
      ];
    };
  };
}
