{ std, lib, actionLib, ... } @ args:

let namespace = "cicero"; in

std.behavior.onInputChange "start" namespace args

{
  inputs.start = ''
    "${namespace}": start: {
      clone_url: string
      sha: string
      statuses_url?: string
    }
  '';

  job = { start }: let
    cfg = start.value.${namespace}.start;
  in std.chain args [
    actionLib.simpleJob

    (lib.optionalAttrs (cfg ? statuses_url)
      (std.github.reportStatus cfg.statuses_url))

    (std.git.clone cfg)

    {
      resources = {
        memory = 4 * 1024;
        cpu = 16000;
      };
    }

    std.nix.develop
    (std.wrapScript "bash" (next: ''
      lint
      ${lib.escapeShellArg next}
    ''))

    std.nix.build
  ];
}
