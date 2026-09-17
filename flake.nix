{
  description = "ailoop — motor de workflow para desarrollo asistido por LLM";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";

  outputs = { self, nixpkgs }:
    let
      forAllSystems = nixpkgs.lib.genAttrs [ "x86_64-linux" "aarch64-linux" ];

      # Herramientas que el loop NO lleva adentro: las descubre en runtime
      # (internal/host). El núcleo es Go puro y se comporta igual en todas
      # partes; esto es la capa que varía según la máquina.
      runtimeTools = pkgs: with pkgs; [
        git # ramas y cambios sin commitear
        poppler-utils # pdftotext, para @archivo.pdf
        grim # captura de pantalla en Wayland, para @screen
        slurp # selección de región, para @screen:select
        wl-clipboard # wl-paste, para @clipboard
      ];
    in
    {
      # Para DESARROLLAR: `nix develop` deja las herramientas en el PATH.
      devShells = forAllSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system};
        in {
          default = pkgs.mkShell {
            packages = with pkgs; [ go gopls ] ++ runtimeTools pkgs;
          };
        });

      # Para DISTRIBUIR: el binario lleva sus dependencias colgadas.
      #
      # La diferencia con el devShell importa. Un devShell resuelve el PATH de
      # quien lo abre; un binario compilado y copiado a otra máquina sigue
      # buscando pdftotext en el PATH de esa máquina. wrapProgram envuelve el
      # binario en un script que le arma el PATH antes de ejecutarlo, así que
      # `nix run github:cRolandoJr/ailoop` funciona sin instalar nada más.
      packages = forAllSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system};
        in {
          default = pkgs.buildGoModule {
            pname = "ailoop";
            version = "0.1.0";
            src = ./.;

            # Hash de las dependencias de Go. Si cambia go.mod y este queda
            # viejo, el build falla con el hash correcto en el mensaje.
            vendorHash = "sha256-T89KLgQNy3ppIdk9GNOj7twzfQAAfpWfZtnUw1jnWS0=";

            nativeBuildInputs = [ pkgs.makeWrapper ];

            postInstall = ''
              wrapProgram $out/bin/ailoop \
                --prefix PATH : ${pkgs.lib.makeBinPath (runtimeTools pkgs)}
            '';

            meta = with pkgs.lib; {
              description = "El LLM propone; el workflow valida y autoriza";
              homepage = "https://github.com/cRolandoJr/ailoop";
              license = licenses.mit;
              mainProgram = "ailoop";
            };
          };
        });
    };
}
