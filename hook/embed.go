// Package hook expõe o script de hook do claude-fleet para o instalador.
//
// O arquivo fleet-hook.sh é a única fonte do script: o binário o embute e o
// subcomando install-hooks grava esse conteúdo no diretório de destino.
package hook

import _ "embed"

// Script é o conteúdo de fleet-hook.sh, embutido no binário.
//
//go:embed fleet-hook.sh
var Script string
