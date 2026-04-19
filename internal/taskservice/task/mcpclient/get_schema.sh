cd /root/lzy/ququchat/internal/taskservice/task/mcpclient && \
go test -v -run '^TestListToolsAllServers$' -count=1 > test_output.log && \
grep -nE '=== RUN|provides|Tool Name|Description|InputSchema|Prompt\[|FAIL|Error:' test_output.log | head -n 400
