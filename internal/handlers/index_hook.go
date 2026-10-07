package handlers

// indexItem é chamado depois que um item é salvo, para gerar o embedding em segundo plano.
// Padrão: não faz nada (testes e execução sem IA).
var indexItem = func(itemID int) {}

// InitIndexer define a função que indexa um item salvo.
func InitIndexer(fn func(itemID int)) {
	if fn != nil {
		indexItem = fn
	}
}

func notifyItemSaved(itemID int) {
	indexItem(itemID)
}
