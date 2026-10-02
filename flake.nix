{
  description = "tachibana — navi's NixOS experiment (minimal boot stage)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs, ... }: {
    nixosConfigurations.tachibana = nixpkgs.lib.nixosSystem {
      system = "x86_64-linux";
      modules = [
        ({ config, pkgs, ... }: {
          # --- boot ---
          boot.loader.systemd-boot.enable = true;
          boot.loader.efi.canTouchEfiVariables = true;

          # --- identity ---
          networking.hostName = "tachibana";
          time.timeZone = "America/Chicago";
          i18n.defaultLocale = "en_US.UTF-8";

          # --- user ---
          users.users.rav3ndust = {
            isNormalUser = true;
            extraGroups = [ "wheel" "networkmanager" "video" "audio" ];
            # Set with: passwd rav3ndust (after first boot)
          };
          security.sudo.wheelNeedsPassword = true;

          # --- the wired: sway (minimal) ---
          programs.sway = {
            enable = true;
            wrapperFeatures.gtk = true;
          };

          # --- waybar + terminal ---
          environment.systemPackages = with pkgs; [
            waybar
            alacritty
            foot # fallback terminal
            rofi # launcher
            dunst # notifications
          ];

          # --- networking ---
          networking.networkmanager.enable = true;

          # --- sound ---
          services.pipewire = {
            enable = true;
            alsa.enable = true;
            pulse.enable = true;
          };

          # --- display manager (simple; SDDM theming comes later) ---
          services.displayManager.sddm.enable = true;

          # --- SSH for headless debugging ---
          services.openssh.enable = true;

          # --- firmware (Cloudbook needs it) ---
          hardware.enableAllFirmware = true;

          system.stateVersion = "25.05";
        })
      ];
    };
  };
}
