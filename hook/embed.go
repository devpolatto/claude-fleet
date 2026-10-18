// Package hook expõe os scripts do claude-fleet para o instalador.
//
// Os arquivos fleet-hook.sh e fleet-statusline.sh são a única fonte de cada
// script: o binário os embute e o subcomando install-hooks grava esse
// conteúdo no diretório de destino.
package hook

import _ "embed"

// Script é o conteúdo de fleet-hook.sh, embutido no binário.
//
//go:embed fleet-hook.sh
var Script string

// StatusLine é o conteúdo de fleet-statusline.sh, embutido no binário.
//
//go:embed fleet-statusline.sh
var StatusLine string
