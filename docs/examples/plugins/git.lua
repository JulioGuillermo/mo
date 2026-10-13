-- A mo plugin: copy it to ~/.config/mo/plugins/ and restart mo.

-- A tool the model can call.
mo.tool{
  name = "git_log",
  description = "Recent commits of the repository, one per line.",
  params = { count = { type = "integer", description = "how many, default 10" } },
  run = function(args)
    local count = tonumber(args.count) or 10
    local output, code = mo.exec("git log --oneline -n " .. math.floor(count))
    if code ~= 0 then error(output) end
    return output
  end,
}

-- A command for you: /standup prints what you committed since yesterday.
mo.command{
  name = "standup",
  description = "My commits since yesterday",
  run = function()
    local author = mo.exec("git config user.email"):gsub("%s+$", ""):gsub("'", "")
    local output = mo.exec("git log --since=yesterday --author='" .. author .. "' --format='- %s'")
    if output == "" then return "_No commits since yesterday._" end
    return "**Since yesterday**\n\n" .. output
  end,
}

-- A guard: block force pushes, also when bash would run them without asking.
mo.on("tool_call", function(call)
  local command = " " .. (call.name == "bash" and call.args.command or "") .. " "
  if command:find("git push") and (command:find("%-%-force") or command:find("%s%-f%s")) then
    return "force pushes are not allowed"
  end
end)

-- Add a reminder to every prompt.
mo.on("prompt", function(text)
  return text .. "\n\n(Use the repository's existing style.)"
end)
