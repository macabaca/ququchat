const BASE_URL: string = "https://domain.com/api/v1"

const WHITE_LIST: Array<string> = ['/auth', '/auth/login']
const REQUEST_URI: Map<string,string> = new Map<string,string>([
    ["login", "/auth/login"],
    ["register", "/auth/register"],
])

export {
    BASE_URL,
    WHITE_LIST,
    REQUEST_URI
}