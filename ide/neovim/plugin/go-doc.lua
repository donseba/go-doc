if vim.g.go_doc_auto_start == false then
  return
end

require("go-doc").setup()
