local M = {}

local defaults = {
  cmd = { "go-doc", "lsp" },
  filetypes = { "gohtml", "gotmpl", "html" },
  autostart = true,
}

local config = vim.deepcopy(defaults)

local function contains(values, value)
  for _, item in ipairs(values) do
    if item == value then
      return true
    end
  end
  return false
end

local function executable(command)
  return vim.fn.executable(command) == 1
end

local function root_dir(bufnr)
  local name = vim.api.nvim_buf_get_name(bufnr)
  local start = name ~= "" and vim.fs.dirname(name) or vim.loop.cwd()
  local marker = vim.fs.find({ "go.mod" }, { path = start, upward = true })[1]
  if marker then
    return vim.fs.dirname(marker)
  end
  return vim.loop.cwd()
end

function M.start(bufnr)
  bufnr = bufnr or vim.api.nvim_get_current_buf()
  if vim.b[bufnr].go_doc_lsp_started then
    return
  end
  local filetype = vim.bo[bufnr].filetype
  if not contains(config.filetypes, filetype) then
    return
  end
  if not executable(config.cmd[1]) then
    vim.notify("go-doc is not available on PATH", vim.log.levels.WARN)
    return
  end

  local root = root_dir(bufnr)
  local cmd = vim.deepcopy(config.cmd)
  table.insert(cmd, root)

  local clientID = vim.lsp.start({
    name = "go-doc",
    cmd = cmd,
    root_dir = root,
  }, { bufnr = bufnr })
  if clientID then
    vim.b[bufnr].go_doc_lsp_started = true
  end
end

function M.setup(opts)
  config = vim.tbl_deep_extend("force", defaults, opts or {})

  if vim.g.go_doc_auto_start == false or config.autostart == false then
    return
  end

  local group = vim.api.nvim_create_augroup("go_doc_lsp", { clear = true })
  vim.api.nvim_create_autocmd("FileType", {
    group = group,
    pattern = config.filetypes,
    callback = function(event)
      M.start(event.buf)
    end,
  })
end

return M
