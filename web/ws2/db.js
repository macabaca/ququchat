;(function() {
    // --- Local-First DB Init ---
    var db = new Dexie("QuQuChatDB");
    db.version(1).stores({
        messages: "id, room_id, sequence_id, created_at, [room_id+sequence_id]"
    });

    window.ChatDB = {
        // Save a single message (handles both raw WS msg and formatted DB msg)
        saveMessage: function(msg) {
            return db.messages.put({
                id: msg.id,
                room_id: msg.room_id,
                sequence_id: msg.sequence_id || 0,
                sender_id: msg.sender_id || msg.from_user_id,
                content_text: msg.content_text || msg.content,
                created_at: msg.created_at || msg.timestamp,
                content_type: msg.content_type || 'text'
            });
        },

        // Bulk save messages (typically from API response)
        bulkSaveMessages: function(messages) {
            return db.messages.bulkPut(messages);
        },

        // Get the last sequence_id for a room
        getLastSequenceId: function(roomId) {
            return db.messages.where({room_id: roomId}).reverse().sortBy('sequence_id')
                .then(function(lastMsgs) {
                    return (lastMsgs.length > 0) ? lastMsgs[0].sequence_id : 0;
                });
        },

        // Get all messages for a room, sorted by sequence_id
        getMessages: function(roomId) {
            return db.messages.where({room_id: roomId}).sortBy('sequence_id');
        }
    };
})();