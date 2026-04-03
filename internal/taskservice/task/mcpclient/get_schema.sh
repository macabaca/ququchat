cd /root/lzy/ququchat/internal/taskservice/task/mcpclient && \
go test -v -run 'Test(Tavily|GezhePPT)ListTools' > test_output.log && \
grep -nE 'provides|Tool Name|Description|InputSchema|Prompt\[' test_output.log | head -n 200