if exists('g:loaded_go_doc_lsp')
  finish
endif
let g:loaded_go_doc_lsp = 1

if exists('g:go_doc_auto_start') && !g:go_doc_auto_start
  finish
endif

function! s:root_dir() abort
  let l:dir = expand('%:p:h')
  while l:dir !=# ''
    if filereadable(l:dir . '/go.mod')
      return l:dir
    endif
    let l:parent = fnamemodify(l:dir, ':h')
    if l:parent ==# l:dir
      break
    endif
    let l:dir = l:parent
  endwhile
  return getcwd()
endfunction

function! s:register_go_doc_lsp() abort
  if !exists('*lsp#register_server') || !executable('go-doc')
    return
  endif

  if exists('s:registered')
    return
  endif
  let s:registered = 1

  call lsp#register_server({
        \ 'name': 'go-doc',
        \ 'cmd': {server_info -> ['go-doc', 'lsp', s:root_dir()]},
        \ 'allowlist': ['gohtml', 'gotmpl', 'html'],
        \ })
endfunction

augroup go_doc_lsp
  autocmd!
  autocmd User lsp_setup call s:register_go_doc_lsp()
augroup END
